package relay

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

const maxPendingStreamBytes = 1 << 20

// streamTruncatedReason 是流式响应被截断时统一对外的失败原因。它同时写入客户端
// 终止帧与用量记录，便于管理端与客户端用同一句话对齐现象。
const streamTruncatedReason = "上游流式响应未正常结束（缺少结束标记）"

// streamTruncated 判断一次转发是否以「已经向客户端写出内容、但没等到结束标记」
// 收尾。这种响应客户端拿到的是半截回答，既不能算成功，也无法切换候选重放。
func streamTruncated(stream bool, result attemptResult) bool {
	return stream && result.wroteBytes && !result.streamCompleted
}

// streamTerminal 描述一次流式截断的收尾信息：对外状态码与可展示的原因。
type streamTerminal struct {
	status  int
	message string
}

// streamTerminalLines 按客户端所在协议生成显式终止帧：一个错误事件加一个结束标记。
// 客户端据此提示「上游中断」，而不是只看到连接被断开后静默停下。
func streamTerminalLines(clientEndpoint string, terminal streamTerminal) [][]byte {
	status := terminal.status
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusBadGateway
	}
	message := strings.TrimSpace(terminal.message)
	if message == "" {
		message = streamTruncatedReason
	}
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "upstream_error",
			"code":    status,
		},
	})
	if err != nil {
		return nil
	}
	if clientEndpoint == protocol.ResponsesEndpoint {
		event, marshalErr := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]any{
				"message": message,
				"type":    "upstream_error",
				"code":    status,
			},
		})
		if marshalErr != nil {
			return nil
		}
		return [][]byte{[]byte("event: error\ndata: " + string(event) + "\n\n")}
	}
	return [][]byte{
		[]byte("data: " + string(payload) + "\n\n"),
		[]byte("data: [DONE]\n\n"),
	}
}

type streamFailure struct {
	status  int
	body    []byte
	message string
}

func parseStreamFailure(line []byte) (streamFailure, bool) {
	payload, done, valid := relaySSEDataPayload(line)
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

func streamPayloadHasVisibleOutput(line []byte) bool {
	payload, done, valid := relaySSEDataPayload(line)
	if !valid || done {
		return false
	}
	eventType := gjson.GetBytes(payload, "type").String()
	switch eventType {
	case "response.output_text.delta", "response.refusal.delta", "response.function_call_arguments.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return strings.TrimSpace(gjson.GetBytes(payload, "delta").String()) != ""
	case "content_block_delta":
		delta := gjson.GetBytes(payload, "delta")
		return strings.TrimSpace(delta.Get("text").String()) != "" ||
			strings.TrimSpace(delta.Get("thinking").String()) != "" ||
			strings.TrimSpace(delta.Get("partial_json").String()) != ""
	case "response.output_item.added":
		return gjson.GetBytes(payload, "item.type").String() == "function_call"
	}
	choice := gjson.GetBytes(payload, "choices.0")
	if !choice.Exists() {
		return false
	}
	delta := choice.Get("delta")
	return strings.TrimSpace(delta.Get("content").String()) != "" ||
		strings.TrimSpace(delta.Get("reasoning_content").String()) != "" ||
		strings.TrimSpace(delta.Get("reasoning").String()) != "" ||
		strings.TrimSpace(delta.Get("thinking").String()) != "" ||
		delta.Get("tool_calls.#").Int() > 0
}
