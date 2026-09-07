package relay

import "time"

// recordFirstStreamOutput tracks the first visible upstream increment, before
// protocol conversion or sensitive-value restoration can delay client writes.
// It must be measured from the moment THIS attempt sent the upstream request
// (requestStartedAt), not from the whole-relay startedAt — otherwise a retried
// attempt inherits the latency of every earlier failed attempt.
func recordFirstStreamOutput(result *attemptResult, requestStartedAt time.Time) {
	if result.firstTokenMs != nil {
		return
	}
	first := time.Since(requestStartedAt).Milliseconds()
	result.firstTokenMs = &first
}
