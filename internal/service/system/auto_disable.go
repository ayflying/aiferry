package system

import (
	"fmt"
	"strings"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

const (
	AutoDisableSourceRelayRequest = "relay_request"
	AutoDisableSourceModelTest    = "model_test"
	autoDisableSourceUnknown      = "unknown"
)

type AutoDisableInput struct {
	ChannelID uint64
	Source    string
	Status    int
	Latency   time.Duration
	Message   string
}

func matchesAutoDisable(settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) bool {
	if input.Status > 0 && MatchesStatusCodeRules(settings.DisableStatusCodes, input.Status) {
		return true
	}
	if input.Latency >= time.Duration(settings.DisableLatencySeconds)*time.Second {
		return true
	}
	message := strings.ToLower(input.Message)
	for _, keyword := range settings.FailureKeywords {
		if strings.Contains(message, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func autoDisableReason(input AutoDisableInput) string {
	parts := make([]string, 0, 3)
	if input.Status > 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", input.Status))
	}
	if input.Latency > 0 {
		parts = append(parts, input.Latency.Round(time.Millisecond).String())
	}
	if message := strings.TrimSpace(input.Message); message != "" {
		parts = append(parts, message)
	}
	return truncate(strings.Join(parts, " · "), 1024)
}

func autoDisableSource(source string) string {
	switch source {
	case AutoDisableSourceRelayRequest, AutoDisableSourceModelTest:
		return source
	default:
		return autoDisableSourceUnknown
	}
}
