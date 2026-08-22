package relay

import (
	"context"
	"net/http"
	"strings"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

type channelAttempt struct {
	candidate Candidate
	result    attemptResult
	handled   bool
	attempts  int
}

// attemptChannel keeps retries inside one channel until no usable upstream key
// remains. Those retries do not consume the cross-channel failover budget.
func (s *sRelay) attemptChannel(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, body []byte, candidate Candidate, stream bool, startedAt time.Time, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput, excluded map[uint64]struct{}, sensitiveDataRestorer *sensitiveDataRestorer) channelAttempt {
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
			result, _, attemptErr := s.attempt(ctx, attemptWriter, incomingHeaders, endpoint, body, current, stream, startedAt, settings, sensitiveDataRestorer)
			result.latency = time.Since(attemptStartedAt)
			last = channelAttempt{candidate: current, result: result, attempts: last.attempts + 1}
			if attemptErr != nil {
				last.result = failedAttemptResult(last.result, attemptErr.Error())
				last.result.timedOut = isUpstreamTimeout(attemptErr)
			}
			if attemptCompleted(last.result, attemptErr) || nonRetryableClientFailure(last.result, attemptErr, settings) {
				last.handled = true
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
	return result.wroteBytes || (attemptErr == nil && result.status >= http.StatusOK && result.status < http.StatusMultipleChoices)
}

// nonRetryableClientFailure 对切换凭据或备用地址也无法修复的请求错误停止重试。
// 仅当上游明确提示接口不支持时保留协议回退，因为切换到另一套 API 后仍可能成功。
func nonRetryableClientFailure(result attemptResult, attemptErr error, settings adminapi.SystemResilienceSettingsInput) bool {
	if attemptErr != nil || result.wroteBytes || result.status < http.StatusBadRequest || result.status >= http.StatusInternalServerError {
		return false
	}
	if retryableStatusForRules(result.status, settings.RetryStatusCodes) {
		return false
	}
	return !shouldFallbackWithProtocolConversion(result.status, result.body)
}
