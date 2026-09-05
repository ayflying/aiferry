package usage

import (
	"encoding/json"
	"strings"
)

func ParseAttemptFlow(raw string) []AttemptFlowStep {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var result []AttemptFlowStep
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil
	}
	return result
}
