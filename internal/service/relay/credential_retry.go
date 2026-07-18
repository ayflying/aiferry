package relay

import (
	"context"
	"net/http"
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
func (s *Service) attemptChannel(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, endpoint string, body []byte, candidate Candidate, stream bool, startedAt time.Time, apiKeyID uint64, settings adminapi.SystemResilienceSettingsInput) channelAttempt {
	excluded := make(map[uint64]struct{})
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
		attemptStartedAt := time.Now()
		attemptWriter := writer
		if !stream {
			attemptWriter = nil
		}
		result, handled, attemptErr := s.attempt(ctx, attemptWriter, incomingHeaders, endpoint, body, current, stream, startedAt, settings)
		result.latency = time.Since(attemptStartedAt)
		last = channelAttempt{candidate: current, result: result, handled: handled, attempts: last.attempts + 1}
		if attemptErr != nil {
			last.result.errorMessage = attemptErr.Error()
			last.result.timedOut = isUpstreamTimeout(attemptErr)
		}
		s.maybeAutoDisable(ctx, settings, current, last.result)
		if handled {
			return last
		}
		if attemptErr == nil && !retryableStatusForRules(result.status, settings.RetryStatusCodes) {
			last.handled = true
			return last
		}
		if result.wroteBytes {
			last.handled = true
			return last
		}
		s.markFailure(ctx, current.ChannelCredentialID)
		excluded[current.ChannelCredentialID] = struct{}{}
	}
}
