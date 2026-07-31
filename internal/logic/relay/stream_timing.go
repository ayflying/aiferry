package relay

import "time"

// recordFirstStreamOutput tracks the first visible upstream increment, before
// protocol conversion or sensitive-value restoration can delay client writes.
func recordFirstStreamOutput(result *attemptResult, startedAt time.Time) {
	if result.firstTokenMs != nil {
		return
	}
	first := time.Since(startedAt).Milliseconds()
	result.firstTokenMs = &first
}
