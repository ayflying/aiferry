package relay

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

const maxPendingStreamBytes = 64 << 20

type streamFailure struct {
	status  int
	body    []byte
	message string
}

func parseStreamFailure(line []byte) (streamFailure, bool) {
	payload, done, valid := sseDataPayload(line)
	if !valid || done {
		return streamFailure{}, false
	}
	body, found := streamFailureBody(payload)
	if !found {
		return streamFailure{}, false
	}
	return streamFailure{
		status:  streamFailureStatus(payload, body),
		body:    body,
		message: upstreamError(body, "upstream stream failed"),
	}, true
}

func streamFailureBody(payload []byte) ([]byte, bool) {
	for _, path := range []string{"error", "response.error"} {
		value := gjson.GetBytes(payload, path)
		if value.Exists() && value.Raw != "null" {
			return []byte(`{"error":` + value.Raw + `}`), true
		}
	}
	eventType := gjson.GetBytes(payload, "type").String()
	status := gjson.GetBytes(payload, "response.status").String()
	if eventType == "error" || eventType == "response.failed" || (eventType == "response.completed" && (status == "failed" || status == "incomplete")) {
		return payload, true
	}
	if gjson.GetBytes(payload, "code").Exists() && gjson.GetBytes(payload, "message").Exists() {
		return payload, true
	}
	return nil, false
}

func streamFailureStatus(payload, body []byte) int {
	for _, path := range []string{"error.status", "response.error.status", "error.code", "response.error.code", "status", "code"} {
		if status := statusCode(gjson.GetBytes(payload, path)); status != 0 {
			return status
		}
	}
	text := strings.ToLower(string(body))
	if strings.Contains(text, "quota") || strings.Contains(text, "balance") || strings.Contains(text, "billing") || strings.Contains(text, "payment") || strings.Contains(text, "insufficient") {
		return http.StatusPaymentRequired
	}
	return http.StatusBadGateway
}

func statusCode(value gjson.Result) int {
	if number := int(value.Int()); number >= 100 && number <= 599 {
		return number
	}
	number, err := strconv.Atoi(strings.TrimSpace(value.String()))
	if err == nil && number >= 100 && number <= 599 {
		return number
	}
	return 0
}
