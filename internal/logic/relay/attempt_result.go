package relay

import (
	"net/http"
	"strings"
)

// failedAttemptResult guarantees that a failed request can be shown with a useful reason.
func failedAttemptResult(result attemptResult, fallback string) attemptResult {
	if strings.TrimSpace(result.errorMessage) == "" {
		result.errorMessage = upstreamError(result.body, fallback)
	}
	if strings.TrimSpace(result.errorMessage) == "" {
		result.errorMessage = "上游渠道请求失败"
	}
	if result.status == 0 || (result.status >= http.StatusOK && result.status < http.StatusMultipleChoices) {
		result.status = http.StatusBadGateway
	}
	if len(result.body) == 0 {
		result.body = openAIError("upstream_error", result.errorMessage)
	}
	return result
}
