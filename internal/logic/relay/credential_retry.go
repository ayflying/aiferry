package relay

import (
	"context"
	"net/http"
	"strings"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

type channelAttempt struct {
	candidate Candidate
	result    attemptResult
	handled   bool
	attempts  int
	flow      []usage.AttemptFlowStep
}

// attemptChannel keeps retries inside one channel until no usable upstream key
// remains. Those retries do not consume the cross-channel failover budget.
func (s *sRelay) attemptChannel(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, body []byte, candidate Candidate, stream bool, startedAt time.Time, userID, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, excluded map[uint64]struct{}, sensitiveDataRestorer *sensitiveDataRestorer) channelAttempt {
	candidate.ReasoningEffort = requestReasoningEffort(body)
	last := channelAttempt{candidate: candidate}
	for {
		credential, err := s.channels.SelectCredential(ctx, apiKeyID, candidate.ChannelID, excluded)
		if err != nil {
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
		for _, baseURL := range candidateBaseURLs(current) {
			current.BaseURL = baseURL
			attemptStartedAt := time.Now()
			attemptWriter := writer
			if !stream {
				attemptWriter = nil
			}
			result, _, attemptErr := s.attempt(ctx, attemptWriter, incomingHeaders, endpoint, body, current, stream, startedAt, userID, settings, sensitiveDataRestorer)
			result.latency = time.Since(attemptStartedAt)
			flow := append(last.flow, usage.AttemptFlowStep{ChannelName: current.ChannelName, DurationMs: result.latency.Milliseconds(), FirstTokenMs: result.firstTokenMs})
			last = channelAttempt{candidate: current, result: result, attempts: last.attempts + 1, flow: flow}
			if attemptErr != nil {
				last.result = failedAttemptResult(last.result, attemptErr.Error())
				last.result.timedOut = isUpstreamTimeout(attemptErr)
			}
			if attemptCompleted(last.result, attemptErr) || nonRetryableClientFailure(last.result, attemptErr, settings) {
				last.handled = true
				last.result.attemptFlow = last.flow
				return last
			}
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

func attemptCompleted(result attemptResult, attemptErr error) bool {
	// Chat/Responses relay requests are expected to return HTTP 200. Any other
	// status is an upstream error and must remain in the retry/failover decision.
	return result.wroteBytes || (attemptErr == nil && result.status == http.StatusOK)
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
