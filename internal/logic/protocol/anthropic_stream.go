package protocol

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// anthropicToChat 把 Anthropic Messages 的 SSE 事件流转成 Chat chunk 流。
// 事件形态：message_start（id/model/输入 usage）→ content_block_start/delta/stop
// （text / thinking / tool_use 三类块）→ message_delta（stop_reason + 输出
// usage）→ message_stop。error 事件在这里只是兜底：正常失败已被 relay 层的
// parseStreamFailure 拦截，轮不到转换器。
func (c *StreamConverter) anthropicToChat(payload []byte) [][]byte {
	chunks := c.anthropicToChatChunks(payload)
	result := make([][]byte, 0, len(chunks)+1)
	for _, chunk := range chunks {
		result = append(result, sseData(chunk))
	}
	// Anthropic 用 message_stop/error 结束流，没有 [DONE] 帧；chat 客户端
	// 依赖它判定流终结，这里在收尾事件后补发。
	switch gjson.GetBytes(payload, "type").String() {
	case "message_stop", "error":
		if len(result) > 0 {
			result = append(result, []byte("data: [DONE]\n\n"))
		}
	}
	return result
}

// anthropicToResponses 让 Responses 客户端复用同一条 Anthropic 上游：
// 先把事件转成 Chat chunk，再把 Chat chunk 送进既有 chat→Responses 流合成，
// usage 与文本收尾因此走同一条路径，不维护第二套 Responses 事件逻辑。
func (c *StreamConverter) anthropicToResponses(payload []byte) [][]byte {
	chunks := c.anthropicToChatChunks(payload)
	var result [][]byte
	for _, chunk := range chunks {
		encoded, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		result = append(result, c.chatToResponses(encoded)...)
	}
	return result
}

func (c *StreamConverter) anthropicToChatChunks(payload []byte) []map[string]any {
	c.captureAnthropicMetadata(payload)
	switch gjsonType := gjson.GetBytes(payload, "type").String(); gjsonType {
	case "message_start":
		message := gjson.GetBytes(payload, "message")
		if usage := message.Get("usage"); usage.Exists() {
			c.usage = anthropicUsageToChat(jsonObject(usage.Raw))
		}
		return c.chatRoleChunks()
	case "content_block_start":
		block := gjson.GetBytes(payload, "content_block")
		if block.Get("type").String() != "tool_use" {
			return nil
		}
		c.sawToolCall = true
		callID := block.Get("id").String()
		index := c.chatToolIndex(callID, anthropicBlockKey(gjson.GetBytes(payload, "index").Int()))
		// 与 Responses 上游同理：名称是增量字段，重复下发会被客户端拼重。
		if c.chatToolNameSent[callID] {
			return nil
		}
		c.chatToolNameSent[callID] = true
		return c.chatRoleAndChunk(map[string]any{"tool_calls": []any{map[string]any{
			"index": index, "id": callID, "type": "function",
			"function": map[string]any{"name": block.Get("name").String()},
		}}})
	case "content_block_delta":
		delta := gjson.GetBytes(payload, "delta")
		switch delta.Get("type").String() {
		case "text_delta":
			text := delta.Get("text").String()
			if text == "" {
				return nil
			}
			c.outputText.WriteString(text)
			return c.chatRoleAndChunk(map[string]any{"content": text})
		case "thinking_delta":
			thinking := delta.Get("thinking").String()
			if thinking == "" {
				return nil
			}
			return c.chatRoleAndChunk(map[string]any{"reasoning_content": thinking})
		case "input_json_delta":
			partial := delta.Get("partial_json").String()
			if partial == "" {
				return nil
			}
			c.sawToolCall = true
			index := c.chatToolIndex("", anthropicBlockKey(gjson.GetBytes(payload, "index").Int()))
			return c.chatRoleAndChunk(map[string]any{"tool_calls": []any{map[string]any{
				"index": index, "type": "function",
				"function": map[string]any{"arguments": partial},
			}}})
		}
		return nil
	case "message_delta":
		if reason := gjson.GetBytes(payload, "delta.stop_reason").String(); reason != "" {
			c.stopReason = reason
		}
		if usage := gjson.GetBytes(payload, "usage"); usage.Exists() {
			c.usage = mergeAnthropicUsage(c.usage, jsonObject(usage.Raw))
		}
		return nil
	case "message_stop":
		return c.completeChunks()
	case "error":
		if c.completed {
			return nil
		}
		c.completed = true
		// 兜底错误帧： Responses 客户端方向也发 Chat 形态错误——极端场景下
		// 让客户端至少拿到结构化错误并正常结束流，好过连接静默挂死。
		return c.errorChunks(jsonObject(gjson.GetBytes(payload, "error").Raw))
	}
	return nil
}

// mergeAnthropicUsage 把 message_delta 的增量 usage 合并进 message_start
// 建立的全量 usage：delta 缺失的字段保持原值。不能直接套
// anthropicUsageToChat——它对缺失字段填 0，会把已有的输入 token 数冲掉。
func mergeAnthropicUsage(target, delta map[string]any) map[string]any {
	if target == nil {
		target = map[string]any{}
	}
	if value, exists := delta["input_tokens"]; exists {
		target["prompt_tokens"] = anthropicInt(value)
		if cached, ok := delta["cache_read_input_tokens"]; ok {
			target["prompt_tokens_details"] = map[string]any{"cached_tokens": cached}
		}
	}
	if value, exists := delta["output_tokens"]; exists {
		target["completion_tokens"] = anthropicInt(value)
	}
	prompt, _ := target["prompt_tokens"].(int)
	completion, _ := target["completion_tokens"].(int)
	target["total_tokens"] = prompt + completion
	return target
}
// captureAnthropicMetadata 从 message_start 事件里取 id/model 供所有 chunk
// 复用；其余事件不带 message 字段，天然幂等。
func (c *StreamConverter) captureAnthropicMetadata(payload []byte) {
	message := gjson.GetBytes(payload, "message")
	if !message.Exists() {
		return
	}
	if c.id == "" {
		c.id = message.Get("id").String()
	}
	if c.model == "" {
		c.model = message.Get("model").String()
	}
}

func (c *StreamConverter) chatRoleChunks() []map[string]any {
	// 用独立标记而非常见的 started：anthropic→responses 方向内部先产 Chat
	// 角色帧再转 Responses 事件，若抢占 started 会让 ensureResponsesStarted
	// 误判已启动、漏发 response.created。
	if c.chatRoleSent {
		return nil
	}
	c.chatRoleSent = true
	return []map[string]any{c.newChatChunk(map[string]any{"role": "assistant"}, nil, nil)}
}

func (c *StreamConverter) chatRoleAndChunk(delta map[string]any) []map[string]any {
	return append(c.chatRoleChunks(), c.newChatChunk(delta, nil, nil))
}

func (c *StreamConverter) newChatChunk(delta map[string]any, finishReason any, usage any) map[string]any {
	chunk := map[string]any{
		"id": c.chatID(), "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	return chunk
}

func (c *StreamConverter) errorChunks(err map[string]any) []map[string]any {
	return append(c.chatRoleChunks(), c.newChatChunk(err, nil, nil))
}

// completeChunks 由 message_stop 事件触发的即时收尾；Complete() 的 EOF 路径
// 会再次调用，anthropicFinished 标记保证只发一轮（不用 completed：那是
// Complete() 入口针对 chat/responses 方向的 guard，anthropic 方向已绕过）。
func (c *StreamConverter) completeChunks() []map[string]any {
	if c.anthropicFinished {
		return nil
	}
	c.anthropicFinished = true
	finishReason := anthropicFinishReason(c.stopReason, c.sawToolCall)
	var usage any
	if c.usage != nil {
		usage = c.usage
	}
	return []map[string]any{c.newChatChunk(map[string]any{}, finishReason, usage)}
}

func (c *StreamConverter) completeAnthropicChatStream() [][]byte {
	var result [][]byte
	for _, chunk := range c.completeChunks() {
		result = append(result, sseData(chunk))
	}
	if c.anthropicFinished && len(result) > 0 {
		return append(result, []byte("data: [DONE]\n\n"))
	}
	return result
}

// completeAnthropicResponsesStream 把收尾 chunk 伪装成 Chat 帧喂给
// chat→Responses 合成逻辑，让 usage 记入响应并补齐 completed 事件。
func (c *StreamConverter) completeAnthropicResponsesStream() [][]byte {
	var result [][]byte
	for _, chunk := range c.completeChunks() {
		encoded, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		result = append(result, c.chatToResponses(encoded)...)
	}
	return append(result, c.completeResponsesStream()...)
}
