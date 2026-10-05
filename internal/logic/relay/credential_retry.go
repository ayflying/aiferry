package relay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

// newAttemptFlowStep snapshots one attempt for the usage-log call flow. The
// status/error are kept so the admin UI can expand a failed step and show why
// the gateway moved on to the next candidate. Errors are redacted and capped —
// full bodies stay in the request's failure log.
func newAttemptFlowStep(channelName string, result attemptResult) usage.AttemptFlowStep {
	step := usage.AttemptFlowStep{
		ChannelName:  channelName,
		Endpoint:     result.upstreamEndpoint,
		DurationMs:   result.latency.Milliseconds(),
		FirstTokenMs: result.firstTokenMs,
	}
	if result.status != 0 && (result.status < http.StatusOK || result.status >= http.StatusMultipleChoices) {
		status := uint(result.status)
		step.Status = &status
		step.Error = truncateFailureLog(redactFailureText(result.errorMessage), 240)
	}
	return step
}

// attemptFlowSteps 组装一次上游调用对应的调用流程步骤：协议回退时首跳的失败在前，
// 本次端点调用的结果在后。返回的步数同时是「上游尝试次数」的计数依据——协议回退
// 代表网关真实发出过两次上游请求，必须按两次计入。
func attemptFlowSteps(channelName string, result attemptResult) []usage.AttemptFlowStep {
	steps := make([]usage.AttemptFlowStep, 0, len(result.precedingFlow)+1)
	steps = append(steps, result.precedingFlow...)
	return append(steps, newAttemptFlowStep(channelName, result))
}

// candidateAttemptPasses 是单次请求允许的候选尝试轮数：第一轮按常规规则，第二轮仅在第一轮
// 「零上游尝试」时作为兜底（忽略组合冷却）触发，因此上限为 2。
const candidateAttemptPasses = 2

// candidateThrottleRetryDelay 是最后一个候选遭遇 429 限流后、再次发起尝试前的退避时长。
// 上游限流文案通常建议「稍后重试」（OpenRouter 等聚合上游的共享池限流即如此），1.5 秒足以
// 错开突发的配额窗口，又不会让已经等过的客户端明显变慢。
const candidateThrottleRetryDelay = 1500 * time.Millisecond

// attemptOptions 控制单次候选尝试的可选行为。两个开关都只服务「已经无路可走」的兜底场景，
// 正常的多候选切换流程一律使用零值。
type attemptOptions struct {
	// ignoreComboCooldown 为真时不做「模型 × 密钥」组合冷却过滤。仅供「本次请求的全部候选
	// 都在组合冷却中、零上游尝试就会直接失败」的兜底轮使用。
	ignoreComboCooldown bool
	// throttleRetry 为真时允许对 429 在同一候选同一密钥上退避重试一次。仅由最后一个候选
	// 携带：上游明确建议稍后重试时给最后一条路一次自愈机会；仍有候选可切换时不额外放大限流。
	throttleRetry bool
}

type channelAttempt struct {
	candidate Candidate
	result    attemptResult
	handled   bool
	attempts  int
	flow      []usage.AttemptFlowStep
	// concurrencyExhausted 表示本次尝试根本没发出上游请求：渠道全部密钥的并发额度
	// 都占满且等待窗口内没有空位。该情况由网关本地判定，不参与渠道失败评分。
	concurrencyExhausted bool
	// skipReason 是本次尝试在选凭证阶段被跳过时的具体原因（渠道层直接给出，例如
	// 「3 把密钥在该模型下全部处于组合冷却」）。仅在从未发出上游请求时非空，
	// 供 Handle 组装「零尝试」诊断文案，避免事后按渠道重查得出相矛盾的结论。
	skipReason string
	// channelDead 表示本次尝试命中了「自动禁用状态码」或「失败关键词」：失败被判定
	// 为渠道/密钥侧不可用（maybeAutoDisable 会按同一套规则处理禁用），本渠道剩余的
	// 备用地址与密钥不再尝试，直接交还外层切换下一候选渠道。
	channelDead bool
}

// attemptChannel keeps retries inside one channel until no usable upstream key
// remains. Those retries do not consume the cross-channel failover budget.
func (s *sRelay) attemptChannel(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, body []byte, candidate Candidate, stream bool, userID, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, excluded map[uint64]struct{}, sensitiveDataRestorer *sensitiveDataRestorer, options attemptOptions) channelAttempt {
	candidate.ReasoningEffort = requestReasoningEffort(body)
	last := channelAttempt{candidate: candidate}
	throttleRetried := false
	for {
		// 占额度必须与选密钥一起做：同渠道其它密钥还有空位时直接换密钥，
		// 全部占满才排队等待，避免把同一个密钥的等待强加给整个渠道。
		credential, release, err := s.acquireKeySlot(ctx, apiKeyID, candidate, excluded, options.ignoreComboCooldown)
		if err != nil {
			// 上游以 429 限流（例如 OpenRouter 共享池的「请稍后重试」）而本候选已没有别的
			// 密钥或备用地址可换：退避一次再试，把上游的建议落地。只由最后一个候选启用、
			// 每个候选最多退避一次；客户端已断开时不再发起上游请求，按原错误路径收尾。
			if shouldRetryThrottledCandidate(options, throttleRetried, last, ctx.Err()) {
				throttleRetried = true
				delete(excluded, last.candidate.ChannelCredentialID)
				if waitForThrottleRetry(ctx) {
					continue
				}
			}
			if errors.Is(err, ErrChannelConcurrencyExhausted) {
				last.concurrencyExhausted = true
				last.result = attemptResult{status: http.StatusTooManyRequests, errorMessage: err.Error(), attemptFlow: last.flow}
				return last
			}
			if last.result.status == 0 {
				last.result.status = http.StatusBadGateway
				last.result.errorMessage = err.Error()
				last.result.body = openAIError("upstream_error", err.Error())
			}
			last.skipReason = credentialSkipReasonFromError(err)
			last.result.attemptFlow = last.flow
			return last
		}
		current := candidate
		current.ChannelCredentialID = credential.ID
		current.APIKeyCipher = credential.APIKeyCipher
		// 单把密钥的尝试放在闭包里，用 defer 归还额度：即使上游调用 panic，
		// 也不会让这把密钥的额度永久丢失（额度泄漏只能靠重启进程恢复）。
		last = func() channelAttempt {
			defer release()
			attempted := last
			for _, baseURL := range candidateBaseURLs(current) {
				current.BaseURL = baseURL
				attemptStartedAt := time.Now()
				attemptWriter := writer
				if !stream {
					attemptWriter = nil
				}
				result, _, attemptErr := s.attempt(ctx, attemptWriter, incomingHeaders, endpoint, body, current, stream, userID, apiKeyID, settings, sensitiveDataRestorer)
				result.latency = time.Since(attemptStartedAt)
				if attemptErr != nil {
					result = failedAttemptResult(result, attemptErr.Error())
					result.timedOut = isUpstreamTimeout(attemptErr)
				}
				// 未写出响应的失败在每次真实尝试后记分；已写出的截流交由外层处理。
				if !result.wroteBytes && !result.writerFailed && ctx.Err() == nil && (result.status != http.StatusOK || attemptErr != nil) {
					s.maybeAutoDisable(ctx, settings, current, result)
				}
				steps := attemptFlowSteps(current.ChannelName, result)
				flow := append(attempted.flow, steps...)
				attempted = channelAttempt{candidate: current, result: result, attempts: attempted.attempts + len(steps), flow: flow}
				if attemptCompleted(attempted.result, attemptErr, stream) {
					attempted.handled = true
					break
				}
				if channelDeadFailure(attempted.result, settings) {
					// 禁用类失败（自动禁用状态码/失败关键词）：渠道内重试只会重演，
					// 不再消耗本渠道剩余的备用地址与密钥，交还外层切换下一候选渠道；
					// handled 保持 false，最终结果由候选耗尽路径透传给客户端。
					attempted.channelDead = true
					break
				}
			}
			return attempted
		}()
		if last.handled {
			last.result.attemptFlow = last.flow
			return last
		}
		if last.channelDead {
			// 禁用类失败默认直接跳渠道；仅最后一个候选的首次 429 例外——上游限流
			// 文案通常建议「稍后重试」，保留一次退避重试的机会，重试仍命中再跳。
			if shouldRetryThrottledCandidate(options, throttleRetried, last, ctx.Err()) {
				throttleRetried = true
				last.channelDead = false
				if waitForThrottleRetry(ctx) {
					continue
				}
			}
			last.result.attemptFlow = last.flow
			return last
		}
		excluded[current.ChannelCredentialID] = struct{}{}
	}
}

// shouldRetryThrottledCandidate 判断一次「挑选密钥失败」是否应改判为 429 退避重试：
// 只有最后一个候选（options.throttleRetry）、本候选尚未退避过、上一次真实尝试确实是 429，
// 且客户端仍在等待时才成立。抽成纯函数便于单测覆盖边界。
func shouldRetryThrottledCandidate(options attemptOptions, throttleRetried bool, last channelAttempt, ctxErr error) bool {
	return !throttleRetried && options.throttleRetry &&
		last.result.status == http.StatusTooManyRequests &&
		last.candidate.ChannelCredentialID > 0 && ctxErr == nil
}

// waitForThrottleRetry 等待一次 429 退避窗口。客户端在退避期间断开时返回 false，
// 调用方按原错误路径收尾，不再发起上游请求。
func waitForThrottleRetry(ctx context.Context) bool {
	timer := time.NewTimer(candidateThrottleRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func candidateBaseURLs(candidate Candidate) []string {
	urls := make([]string, 0, len(candidate.BackupBaseURLs)+1)
	seen := make(map[string]struct{}, len(candidate.BackupBaseURLs)+1)
	for _, value := range append([]string{candidate.BaseURL}, candidate.BackupBaseURLs...) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	return urls
}

func attemptCompleted(result attemptResult, attemptErr error, stream bool) bool {
	// Chat/Responses relay requests are expected to return HTTP 200. Any other
	// status is an upstream error and must remain in the retry/failover decision.
	if result.wroteBytes {
		// 已经向客户端写出内容，无法重放，即使随后失败也只能就地收尾。
		return true
	}
	if attemptErr != nil || result.status != http.StatusOK {
		return false
	}
	if stream && !result.streamCompleted {
		// 流式响应没有任何内容落地也没等到结束标记，等同一次失败的上游调用，
		// 允许继续尝试下一个候选。
		return false
	}
	return true
}

// channelDeadFailure 判定一次失败是否意味着「当前渠道已不可用，应整渠道跳过」：
// 状态码命中自动禁用状态码（DisableStatusCodes，与 maybeAutoDisable 的禁用门禁同一套
// MatchesStatusCodeRules 匹配），或错误文案命中失败关键词（FailureKeywords，欠费/配额/
// 鉴权类账号级故障）。这两类失败即使换本渠道的其余密钥或备用地址也大概率重演，因此
// 渠道内不再重试；禁用动作仍由 maybeAutoDisable 负责。
//
// 未命中上述规则的失败一律留在重试链里（备用地址→换密钥→换候选渠道）：上游会把渠道侧
// 故障包装成普通 400（如聚合层的 "Upstream request failed"），4xx 不再被当成对客户端
// 请求的最终判决；全部候选耗尽后由 Handle 把最后一次上游 4xx 原样透传。reasoning_content
// 会话校验拒绝、协议不支持回退等情形也随之并入普通重试——需要跳渠道时，管理员可把相应
// 关键词加入 FailureKeywords。
func channelDeadFailure(result attemptResult, settings adminapi.SystemResilienceSettingsInput) bool {
	if result.wroteBytes {
		// 已经向客户端写出内容的结果由 attemptCompleted 就地收尾，不参与重试分类。
		return false
	}
	if result.status >= http.StatusBadRequest && system.MatchesStatusCodeRules(settings.DisableStatusCodes, result.status) {
		return true
	}
	message := strings.ToLower(result.errorMessage)
	if message == "" {
		return false
	}
	for _, keyword := range settings.FailureKeywords {
		if strings.Contains(message, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// credentialSkipReasonFromError 从选凭证失败的错误里取出可直接展示的原因。渠道层能区分
// 「模型 × 密钥组合冷却」「渠道级凭证冷却」「无启用密钥」「本次请求已排除全部密钥」等具体
// 情形；不认识的错误（数据库、Redis、上下文取消）返回空串，由调用方兜底，不把内部错误
// 原文写进面向排障的文案。
func credentialSkipReasonFromError(err error) string {
	var unavailable *channel.CredentialUnavailableError
	if errors.As(err, &unavailable) {
		return unavailable.Reason
	}
	return ""
}
