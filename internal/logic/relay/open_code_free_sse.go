package relay

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// aggregateOpenCodeFreeSSE 把 OpenCode Zen 免费层强制流式后返回的 chat SSE
// 聚合成非流式 chat.completion JSON。免费层指纹校验要求 stream=true，客户端
// 非流式请求被网关改写后必须在这里把分片拼回完整响应，才能走既有非流式回包、
// 计费与报文落盘路径。聚合失败时原样返回，交由上层按上游原始响应处理。
func aggregateOpenCodeFreeSSE(raw []byte) []byte {
	content := ""
	reasoning := ""
	role := "assistant"
	model := ""
	id := ""
	created := int64(0)
	finishReason := ""
	toolCalls := map[int]map[string]any{}
	usage := gjson.Result{}
	sawFrame := false

	for _, line := range splitSSELines(raw) {
		payload, done, valid := relaySSEDataPayload(line)
		if !valid || done {
			continue
		}
		sawFrame = true
		if v := gjson.GetBytes(payload, "model").String(); v != "" {
			model = v
		}
		if v := gjson.GetBytes(payload, "id").String(); v != "" {
			id = v
		}
		if v := gjson.GetBytes(payload, "created").Int(); v != 0 {
			created = v
		}
		if u := gjson.GetBytes(payload, "usage"); u.Exists() && u.Raw != "" && u.Raw != "null" {
			usage = u
		}
		if v := gjson.GetBytes(payload, "choices.0.finish_reason").String(); v != "" {
			finishReason = v
		}
		delta := gjson.GetBytes(payload, "choices.0.delta")
		if !delta.Exists() {
			// 末帧可能直接给完整 message 而不是 delta。
			if message := gjson.GetBytes(payload, "choices.0.message"); message.Exists() {
				content += message.Get("content").String()
				reasoning += message.Get("reasoning_content").String()
			}
			continue
		}
		if v := delta.Get("role").String(); v != "" {
			role = v
		}
		content += delta.Get("content").String()
		reasoning += delta.Get("reasoning_content").String()
		for _, call := range delta.Get("tool_calls").Array() {
			index := int(call.Get("index").Int())
			slot, ok := toolCalls[index]
			if !ok {
				slot = map[string]any{
					"index":    index,
					"id":       call.Get("id").String(),
					"type":     "function",
					"function": map[string]any{"name": "", "arguments": ""},
				}
				toolCalls[index] = slot
			}
			if v := call.Get("id").String(); v != "" && slot["id"] == "" {
				slot["id"] = v
			}
			function, _ := slot["function"].(map[string]any)
			function["name"] = function["name"].(string) + call.Get("function.name").String()
			function["arguments"] = function["arguments"].(string) + call.Get("function.arguments").String()
		}
	}

	message := map[string]any{"role": role, "content": content}
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(toolCalls) > 0 {
		ordered := make([]any, 0, len(toolCalls))
		for i := 0; i < len(toolCalls); i++ {
			if slot, ok := toolCalls[i]; ok {
				ordered = append(ordered, slot)
			}
		}
		message["tool_calls"] = ordered
	}
	choice := map[string]any{"index": 0, "message": message}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	result := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{choice},
	}
	if usage.Raw != "" {
		result["usage"] = json.RawMessage(usage.Raw)
	}
	if !sawFrame {
		// 非 SSE 或空响应：原样返回，交由上层按上游原始响应处理（错误透传等）。
		return raw
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return raw
	}
	return encoded
}

// splitSSELines 按换行切分 SSE 文本，去掉行尾 \r。
func splitSSELines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			end := i
			if end > start && raw[end-1] == '\r' {
				end--
			}
			if end > start {
				lines = append(lines, raw[start:end])
			}
			start = i + 1
		}
	}
	return lines
}
