package relay

import (
	"encoding/json"
	"strings"
	"time"
)

func responsesResponseToChat(body []byte) []byte {
	source, err := decodeProtocolObject(body)
	if err != nil {
		return body
	}
	message := map[string]any{"role": "assistant"}
	var (
		content   strings.Builder
		refusal   string
		toolCalls []any
	)
	for _, value := range arrayValue(source["output"]) {
		item, ok := objectValue(value)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "message":
			for _, partValue := range arrayValue(item["content"]) {
				part, ok := objectValue(partValue)
				if !ok {
					continue
				}
				switch stringValue(part["type"]) {
				case "output_text", "input_text", "text":
					content.WriteString(stringValue(part["text"]))
				case "refusal":
					refusal += stringValue(part["refusal"])
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, map[string]any{
				"id":   stringValue(item["call_id"]),
				"type": "function",
				"function": map[string]any{
					"name":      stringValue(item["name"]),
					"arguments": stringValue(item["arguments"]),
				},
			})
		}
	}
	if content.Len() > 0 || len(toolCalls) == 0 {
		message["content"] = content.String()
	} else {
		message["content"] = nil
	}
	if refusal != "" {
		message["refusal"] = refusal
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	} else if stringValue(source["status"]) == "incomplete" {
		finishReason = "length"
	}
	target := map[string]any{
		"id":      stringValue(source["id"]),
		"object":  "chat.completion",
		"created": protocolCreated(source, "created_at"),
		"model":   stringValue(source["model"]),
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}},
	}
	if usage, ok := objectValue(source["usage"]); ok {
		target["usage"] = responseUsageToChat(usage)
	}
	result, err := json.Marshal(target)
	if err != nil {
		return body
	}
	return result
}

func chatResponseToResponses(body []byte) []byte {
	source, err := decodeProtocolObject(body)
	if err != nil {
		return body
	}
	output := make([]any, 0)
	for _, value := range arrayValue(source["choices"]) {
		choice, ok := objectValue(value)
		if !ok {
			continue
		}
		message, ok := objectValue(choice["message"])
		if !ok {
			continue
		}
		content := chatContentToResponseOutput(message["content"])
		if refusal := stringValue(message["refusal"]); refusal != "" {
			content = append(content, map[string]any{"type": "refusal", "refusal": refusal})
		}
		output = append(output, map[string]any{"type": "message", "role": stringOr(message["role"], "assistant"), "status": "completed", "content": content})
		for _, toolValue := range arrayValue(message["tool_calls"]) {
			tool, ok := objectValue(toolValue)
			if !ok {
				continue
			}
			function, _ := objectValue(tool["function"])
			output = append(output, map[string]any{"type": "function_call", "call_id": stringValue(tool["id"]), "name": stringValue(function["name"]), "arguments": stringValue(function["arguments"]), "status": "completed"})
		}
	}
	target := map[string]any{
		"id":         stringValue(source["id"]),
		"object":     "response",
		"created_at": protocolCreated(source, "created"),
		"status":     "completed",
		"model":      stringValue(source["model"]),
		"output":     output,
	}
	if usage, ok := objectValue(source["usage"]); ok {
		target["usage"] = chatUsageToResponse(usage)
	}
	result, err := json.Marshal(target)
	if err != nil {
		return body
	}
	return result
}

func chatContentToResponseOutput(value any) []any {
	if text, ok := value.(string); ok {
		return []any{map[string]any{"type": "output_text", "text": text}}
	}
	result := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		if stringValue(item["type"]) == "text" {
			result = append(result, map[string]any{"type": "output_text", "text": stringValue(item["text"])})
			continue
		}
		result = append(result, item)
	}
	return result
}

func responseUsageToChat(usage map[string]any) map[string]any {
	target := copyProtocolFields(usage, "total_tokens")
	if value, exists := usage["input_tokens"]; exists {
		target["prompt_tokens"] = value
	}
	if value, exists := usage["output_tokens"]; exists {
		target["completion_tokens"] = value
	}
	if details, ok := objectValue(usage["input_tokens_details"]); ok {
		target["prompt_tokens_details"] = details
	}
	return target
}

func chatUsageToResponse(usage map[string]any) map[string]any {
	target := copyProtocolFields(usage, "total_tokens")
	if value, exists := usage["prompt_tokens"]; exists {
		target["input_tokens"] = value
	}
	if value, exists := usage["completion_tokens"]; exists {
		target["output_tokens"] = value
	}
	if details, ok := objectValue(usage["prompt_tokens_details"]); ok {
		target["input_tokens_details"] = details
	}
	return target
}

func protocolCreated(payload map[string]any, field string) int64 {
	if value, ok := payload[field].(float64); ok && value > 0 {
		return int64(value)
	}
	return time.Now().Unix()
}
