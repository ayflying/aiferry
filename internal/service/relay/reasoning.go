package relay

import (
	"strings"

	"github.com/tidwall/gjson"
)

const maxReasoningEffortLength = 32

func requestReasoningEffort(body []byte) string {
	for _, path := range []string{"reasoning_effort", "reasoning.effort"} {
		result := gjson.GetBytes(body, path)
		if result.Type != gjson.String {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(result.String()))
		if value == "" {
			continue
		}
		runes := []rune(value)
		if len(runes) > maxReasoningEffortLength {
			value = string(runes[:maxReasoningEffortLength])
		}
		return value
	}
	return ""
}
