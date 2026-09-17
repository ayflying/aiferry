package protocol

import (
	"encoding/json"
	"strings"
)

// anthropicDefaultMaxTokens 是 Messages 协议必填的 max_tokens 兜底值：
// Anthropic 不提供缺省值，客户端未显式指定时不补会被上游整单拒绝。
const anthropicDefaultMaxTokens = 4096

// chatRequestToAnthropic 把 OpenAI Chat Completions 请求体转成 Anthropic
// Messages 载荷。要点：
//   - system/developer 消息上移为顶层 system 字段；
//   - assistant 的 tool_calls 还原成 tool_use 内容块，role=tool 还原成 user
//     消息里的 tool_result 块，tool_use_id 与发起时的 id 一一对应；
//   - 相邻同角色消息合并成一条：role=tool 转出的 user 消息极易与相邻用户
//     消息撞车，而 Anthropic 要求 user/assistant 严格交替，连续同角色整单拒绝。
func chatRequestToAnthropic(body []byte) ([]byte, error) {
	source, err := decodeProtocolObject(body)
	if err != nil {
		return nil, err
	}
	target := map[string]any{}
	for _, field := range []string{"model", "stream", "temperature", "top_p"} {
		if value, exists := source[field]; exists {
			target[field] = value
		}
	}
	if value, exists := source["max_tokens"]; exists {
		target["max_tokens"] = value
	} else if value, exists := source["max_completion_tokens"]; exists {
		target["max_tokens"] = value
	} else {
		target["max_tokens"] = anthropicDefaultMaxTokens
	}
	if stop := anthropicStopSequences(source["stop"]); len(stop) > 0 {
		target["stop_sequences"] = stop
	}
	messages := make([]any, 0, 8)
	var systemText strings.Builder
	for _, value := range arrayValue(source["messages"]) {
		message, ok := objectValue(value)
		if !ok {
			continue
		}
		switch role := stringValue(message["role"]); role {
		case "system", "developer":
			if text := protocolText(message["content"]); text != "" {
				if systemText.Len() > 0 {
					systemText.WriteString("\n\n")
				}
				systemText.WriteString(text)
			}
		case "tool":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type":        "tool_result",
					"tool_use_id": stringValue(message["tool_call_id"]),
					"content":     anthropicToolResultContent(message["content"]),
				}},
			})
		case "assistant":
			if blocks := anthropicAssistantBlocks(message); len(blocks) > 0 {
				messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
			}
		default:
			if role == "" {
				role = "user"
			}
			if blocks := anthropicUserBlocks(message["content"]); len(blocks) > 0 {
				messages = append(messages, map[string]any{"role": role, "content": blocks})
			}
		}
	}
	messages = mergeAdjacentAnthropicMessages(messages)
	if systemText.Len() > 0 {
		target["system"] = systemText.String()
	}
	if len(messages) > 0 {
		target["messages"] = messages
	}
	// Chat 的 tool_choice=none 表示本次禁止调用工具：Anthropic 没有等价开关，
	// 直接不透传 tools 与 tool_choice，缺省语义即「模型自行决定」。
	if stringValue(source["tool_choice"]) != "none" {
		if tools := anthropicTools(source["tools"]); len(tools) > 0 {
			target["tools"] = tools
		}
		if choice := anthropicToolChoice(source["tool_choice"]); choice != nil {
			target["tool_choice"] = choice
		}
	}
	return encodeProtocolObject(target)
}

// responsesRequestToAnthropic 让 Responses 客户端复用同一条 Anthropic 上游：
// 先降级成 Chat 形态，再走 chat→Anthropic 转换，避免维护第二套消息映射。
func responsesRequestToAnthropic(body []byte) ([]byte, error) {
	chatBody, err := responsesRequestToChat(body)
	if err != nil {
		return nil, err
	}
	return chatRequestToAnthropic(chatBody)
}

func anthropicStopSequences(value any) []any {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
		return []any{typed}
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			if text := stringValue(item); text != "" {
				result = append(result, text)
			}
		}
		return result
	}
	return nil
}

// anthropicUserBlocks 把 Chat 用户内容转成 Anthropic 内容块。文本直达；
// 图片按 data URL（base64）与普通 URL 两种来源分别映射，其余没有
// Anthropic 等价形态的内容块（音频等）跳过，不喂给严格校验的上游。
func anthropicUserBlocks(value any) []any {
	if text, ok := value.(string); ok {
		if text == "" {
			return nil
		}
		return []any{map[string]any{"type": "text", "text": text}}
	}
	blocks := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "text":
			if text := stringValue(item["text"]); text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
		case "image_url":
			if block := anthropicImageBlock(item["image_url"]); block != nil {
				blocks = append(blocks, block)
			}
		}
	}
	return blocks
}

func anthropicImageBlock(value any) map[string]any {
	url := ""
	if image, ok := objectValue(value); ok {
		url = stringValue(image["url"])
	} else {
		url = stringValue(value)
	}
	if url == "" {
		return nil
	}
	if rest, found := strings.CutPrefix(url, "data:"); found {
		comma := strings.Index(rest, ",")
		if comma > 0 {
			return map[string]any{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": strings.TrimSuffix(rest[:comma], ";base64"),
				"data":       rest[comma+1:],
			}}
		}
		return nil
	}
	return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": url}}
}

// anthropicAssistantBlocks 还原 assistant 历史：文本块在前，tool_calls
// 依次转成 tool_use 块（arguments 字符串必须还原成 input 对象）。
func anthropicAssistantBlocks(message map[string]any) []any {
	blocks := make([]any, 0)
	if text, ok := message["content"].(string); ok {
		if text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
	} else {
		for _, itemValue := range arrayValue(message["content"]) {
			item, ok := objectValue(itemValue)
			if !ok {
				continue
			}
			if text := stringValue(item["text"]); text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
		}
	}
	for _, itemValue := range arrayValue(message["tool_calls"]) {
		item, ok := objectValue(itemValue)
		if !ok || stringValue(item["type"]) != "function" {
			continue
		}
		function, ok := objectValue(item["function"])
		if !ok {
			continue
		}
		input := map[string]any{}
		if raw := stringValue(function["arguments"]); strings.TrimSpace(raw) != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		blocks = append(blocks, map[string]any{
			"type": "tool_use", "id": stringValue(item["id"]),
			"name": stringValue(function["name"]), "input": input,
		})
	}
	return blocks
}

func anthropicToolResultContent(value any) any {
	if text, ok := value.(string); ok {
		return text
	}
	blocks := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		if text := stringValue(item["text"]); text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	return blocks
}

func mergeAdjacentAnthropicMessages(messages []any) []any {
	result := make([]any, 0, len(messages))
	for _, value := range messages {
		message, ok := objectValue(value)
		if !ok {
			continue
		}
		if len(result) > 0 {
			last, ok := objectValue(result[len(result)-1])
			if ok && stringValue(last["role"]) == stringValue(message["role"]) {
				last["content"] = append(arrayValue(last["content"]), arrayValue(message["content"])...)
				continue
			}
		}
		result = append(result, message)
	}
	return result
}

// anthropicTools 把 Chat 的 function 工具定义转成 Anthropic 形态。
// input_schema 必填且要求 object 类型，缺失时给空对象兜底。
func anthropicTools(value any) []any {
	result := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		function, ok := objectValue(item["function"])
		if !ok {
			continue
		}
		schema, ok := objectValue(function["parameters"])
		if !ok {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tool := map[string]any{"name": stringValue(function["name"]), "input_schema": schema}
		if description := stringValue(function["description"]); description != "" {
			tool["description"] = description
		}
		result = append(result, tool)
	}
	return result
}

// anthropicToolChoice 映射 Chat 的对象形态 tool_choice；字符串形态
// （auto/required）由调用处直接映射，这里只处理 {type:function}。
func anthropicToolChoice(value any) map[string]any {
	choice, ok := objectValue(value)
	if !ok {
		return nil
	}
	function, ok := objectValue(choice["function"])
	if !ok {
		return nil
	}
	name := stringValue(function["name"])
	if name == "" {
		return nil
	}
	return map[string]any{"type": "tool", "name": name}
}
