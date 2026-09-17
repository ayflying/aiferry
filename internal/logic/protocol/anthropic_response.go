package protocol

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// anthropicResponseToChat 把 Anthropic Messages 非流式响应转回 Chat。
// thinking 块映射为 reasoning_content，tool_use 块映射为 tool_calls，
// stop_reason 按 Chat 语义收敛（end_turn/stop_sequence→stop 等）。
func anthropicResponseToChat(body []byte) []byte {
	source, err := decodeProtocolObject(body)
	if err != nil {
		return body
	}
	message := map[string]any{"role": "assistant"}
	var (
		content   strings.Builder
		reasoning strings.Builder
		toolCalls []any
	)
	for _, value := range arrayValue(source["content"]) {
		item, ok := objectValue(value)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "text":
			content.WriteString(stringValue(item["text"]))
		case "thinking":
			reasoning.WriteString(stringValue(item["thinking"]))
		case "tool_use":
			arguments, err := json.Marshal(item["input"])
			if err != nil {
				arguments = []byte("{}")
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   stringValue(item["id"]),
				"type": "function",
				"function": map[string]any{
					"name":      stringValue(item["name"]),
					"arguments": string(arguments),
				},
			})
		}
	}
	if content.Len() > 0 || len(toolCalls) == 0 {
		message["content"] = content.String()
	} else {
		message["content"] = nil
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	target := map[string]any{
		"id":      stringValue(source["id"]),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   stringValue(source["model"]),
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": anthropicFinishReason(stringValue(source["stop_reason"]), len(toolCalls) > 0),
		}},
	}
	if usage, ok := objectValue(source["usage"]); ok {
		target["usage"] = anthropicUsageToChat(usage)
	}
	result, err := json.Marshal(target)
	if err != nil {
		return body
	}
	return result
}

// anthropicFinishReason 把 Anthropic stop_reason 收敛成 Chat finish_reason。
// 工具调用优先：sawToolCall 时无论 stop_reason 是什么都返回 tool_calls，
// 与 OpenAI 客户端的解析习惯一致。
func anthropicFinishReason(stopReason string, hasToolCall bool) string {
	if hasToolCall {
		return "tool_calls"
	}
	switch stopReason {
	case "max_tokens":
		return "length"
	case "refusal":
		return "content_filter"
	default:
		return "stop"
	}
}

// anthropicUsageToChat 统一流式/非流式共用的 usage 映射：缓存命中单独放进
// prompt_tokens_details，不混入 prompt_tokens 总数。
func anthropicUsageToChat(usage map[string]any) map[string]any {
	prompt := anthropicInt(usage["input_tokens"])
	completion := anthropicInt(usage["output_tokens"])
	target := map[string]any{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
	}
	if cached, exists := usage["cache_read_input_tokens"]; exists {
		target["prompt_tokens_details"] = map[string]any{"cached_tokens": cached}
	}
	return target
}

func anthropicInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int64:
		return int(typed)
	case int:
		return typed
	}
	return 0
}

// anthropicBlockKey 流式侧用 Anthropic 的内容块下标作 chat tool_calls 下标
// 的查找别名：input_json_delta 只带 index 不带工具 id，必须靠它对上号。
func anthropicBlockKey(index int64) string {
	return "block:" + strconv.FormatInt(index, 10)
}
