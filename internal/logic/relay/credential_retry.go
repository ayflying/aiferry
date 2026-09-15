package relay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

// newAttemptFlowStep snapshots one attempt for the usage-log call flow. The
// status/error are kept so the admin UI can expand a failed step and show why
// the gateway moved on to the next candidate. Errors are redacted and capped —
// full bodies stay in the request's failure log.
func newAttemptFlowStep(channelName string, result attemptResult) usage.AttemptFlowStep {
	step := usage.AttemptFlowStep{ChannelName: channelName, DurationMs: result.latency.Milliseconds(), FirstTokenMs: result.firstTokenMs}
	if result.status != 0 && (result.status < http.StatusOK || result.status >= http.StatusMultipleChoices) {
		status := uint(result.status)
		step.Status = &status
		step.Error = truncateFailureLog(redactFailureText(result.errorMessage), 240)
	}
	return step
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
}

// attemptChannel keeps retries inside one channel until no usable upstream key
// remains. Those retries do not consume the cross-channel failover budget.
func (s *sRelay) attemptChannel(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, body []byte, candidate Candidate, stream bool, userID, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, excluded map[uint64]struct{}, sensitiveDataRestorer *sensitiveDataRestorer) channelAttempt {
	candidate.ReasoningEffort = requestReasoningEffort(body)
	last := channelAttempt{candidate: candidate}
	for {
		// 占额度必须与选密钥一起做：同渠道其它密钥还有空位时直接换密钥，
		// 全部占满才排队等待，避免把同一个密钥的等待强加给整个渠道。
		credential, release, err := s.acquireKeySlot(ctx, apiKeyID, candidate, excluded)
		if err != nil {
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
				flow := append(attempted.flow, newAttemptFlowStep(current.ChannelName, result))
				attempted = channelAttempt{candidate: current, result: result, attempts: attempted.attempts + 1, flow: flow}
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
		s.maybeAutoDisable(ctx, settings, current, last.result)
		excluded[current.ChannelCredentialID] = struct{}{}
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
// 只有明确表示接口不支持的响应才允许协议回退。
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
