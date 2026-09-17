package relay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/protocol"
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
				if attemptCompleted(attempted.result, attemptErr, stream) || nonRetryableClientFailure(attempted.result, attemptErr, settings) {
					attempted.handled = true
					break
				}
			}
			return attempted
		}()
		if last.handled {
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

// nonRetryableClientFailure 对切换凭据或备用地址也无法修复的请求错误停止重试。
// 上游鉴权失败表示当前密钥不可用，不能继续用同一请求轮换密钥或地址；
// 只有明确表示接口不支持的响应才允许协议回退；另有两类 4xx 虽然也是 4xx，
// 但成因在渠道侧，同样放行给下一个候选：状态会话校验拒绝（见
// upstreamStatefulSessionRejection）与余额不足 402（账号级失败，换渠道自愈
// 并触发密钥级禁用）。
func nonRetryableClientFailure(result attemptResult, attemptErr error, settings adminapi.SystemResilienceSettingsInput) bool {
	if attemptErr != nil || result.wroteBytes || result.status < http.StatusBadRequest || result.status >= http.StatusInternalServerError {
		return false
	}
	// A received 4xx response is a definitive response for this request after
	// the one in-attempt protocol fallback (if applicable). Do not send the
	// same user payload again with another credential or backup URL. Only the
	// explicitly transient client statuses remain eligible for retry.
	if result.status >= http.StatusBadRequest && result.status < http.StatusInternalServerError {
		if (result.status == http.StatusBadRequest || result.status == http.StatusUnprocessableEntity) && protocol.ShouldFallback(result.status, result.body) {
			return false
		}
		// 有状态会话校验类拒绝的根源在渠道侧，不在请求体：换候选渠道重放即可自愈，
		// 不能当成对客户端请求的最终判决（详见 upstreamStatefulSessionRejection）。
		if upstreamStatefulSessionRejection(result.status, result.body, result.errorMessage) {
			return false
		}
		// 402（余额不足）是账号级失败：成因在渠道密钥，与客户端请求无关。放行给候选
		// 循环换渠道自愈；同时让 maybeAutoDisable 走密钥级禁用分支（isCredentialScopedFailure
		// 已把 402 归为凭证级，见 model_health.go），避免后续请求继续撞同一面余额墙。
		if result.status == http.StatusPaymentRequired {
			return false
		}
		switch result.status {
		case http.StatusNotFound, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
			// 404 can mean that this channel/group does not expose the requested
			// model. Allow routing to the next candidate, but never treat it as a
			// successful response. The configured rule still controls whether the
			// current credential may be retried.
			return !retryableStatusForRules(result.status, settings.RetryStatusCodes)
		default:
			return true
		}
	}
	return false
}

// statefulSessionRejectionField 是上游在有状态会话校验失败时点名的字段。各家措辞不同
// （DeepSeek：「The `reasoning_content` in the thinking mode must be passed back to the
// API.」；Kimi：「thinking is enabled but reasoning_content is missing in assistant tool
// call message at index N」），但都会点名这个字段，因此只认字段名、不绑定整句——宁可在
// 上游改措辞后多换一次渠道，也不要漏判成「客户端请求错」。
const statefulSessionRejectionField = "reasoning_content"

// upstreamStatefulSessionRejection 判定一次 4xx 响应是否为「上游按有状态会话校验，拒绝了
// 本会话的工具调用历史」。
//
// Console Go 系的聚合上游（生产中为 sub2api 渠道）只承认自己生成过的 tool_call 记录：
// 认不出就返回 400，并提示思考内容必须回传。这类失败与请求体本身无关——实测同一份请求
// 换到无状态上游（ch30/31/32 共 63 次）全部 200，而空串兜底在该渠道无效（回传「缺字段 /
// 空串 / 真实内容」的 400 率为 5/10、5/10、4/10，与内容无关）。因此这里必须放行到下一个
// 候选，否则会把一个本可自愈的渠道问题直接暴露给用户，且与多渠道无状态轮转天然冲突。
//
// 上游把错误写进 error.message，个别链路只留下纯文本错误信息，三者都查。
func upstreamStatefulSessionRejection(status int, body []byte, errorMessage string) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	for _, candidate := range []string{gjson.GetBytes(body, "error.message").String(), string(body), errorMessage} {
		if strings.Contains(strings.ToLower(candidate), statefulSessionRejectionField) {
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
