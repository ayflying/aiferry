package relay

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type protocolStreamConverter struct {
	plan           protocolPlan
	id             string
	model          string
	created        int64
	started        bool
	contentStarted bool
	completed      bool
	sawToolCall    bool
	outputText     strings.Builder
	usage          map[string]any
}

func newProtocolStreamConverter(plan protocolPlan) *protocolStreamConverter {
	return &protocolStreamConverter{plan: plan, created: time.Now().Unix()}
}

func (c *protocolStreamConverter) Transform(line []byte) [][]byte {
	payload, done, valid := sseDataPayload(line)
	if !valid {
		return nil
	}
	if done {
		return c.Complete()
	}
	switch c.plan.conversion {
	case chatToResponsesConversion:
		return c.responsesToChat(payload)
	case responsesToChatConversion:
		return c.chatToResponses(payload)
	default:
		return [][]byte{line}
	}
}

func (c *protocolStreamConverter) Complete() [][]byte {
	if c.completed {
		return nil
	}
	c.completed = true
	switch c.plan.conversion {
	case chatToResponsesConversion:
		return c.completeChatStream()
	case responsesToChatConversion:
		return c.completeResponsesStream()
	default:
		return nil
	}
}

func (c *protocolStreamConverter) responsesToChat(payload []byte) [][]byte {
	c.captureResponseMetadata(payload)
	eventType := gjson.GetBytes(payload, "type").String()
	switch eventType {
	case "response.output_text.delta":
		delta := gjson.GetBytes(payload, "delta").String()
		if delta == "" {
			return nil
		}
		c.outputText.WriteString(delta)
		return append(c.ensureChatRole(), c.chatChunk(map[string]any{"content": delta}, nil, nil)...)
	case "response.refusal.delta":
		delta := gjson.GetBytes(payload, "delta").String()
		if delta == "" {
			return nil
		}
		return append(c.ensureChatRole(), c.chatChunk(map[string]any{"refusal": delta}, nil, nil)...)
	case "response.output_item.added":
		item := gjson.GetBytes(payload, "item")
		if item.Get("type").String() != "function_call" {
			return nil
		}
		c.sawToolCall = true
		return append(c.ensureChatRole(), c.chatChunk(map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"id":       item.Get("call_id").String(),
			"type":     "function",
			"function": map[string]any{"name": item.Get("name").String()},
		}}}, nil, nil)...)
	case "response.function_call_arguments.delta":
		delta := gjson.GetBytes(payload, "delta").String()
		if delta == "" {
			return nil
		}
		c.sawToolCall = true
		return append(c.ensureChatRole(), c.chatChunk(map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"id":       gjson.GetBytes(payload, "call_id").String(),
			"type":     "function",
			"function": map[string]any{"arguments": delta},
		}}}, nil, nil)...)
	case "response.completed":
		if usage := gjson.GetBytes(payload, "response.usage"); usage.Exists() {
			c.usage = jsonObject(usage.Raw)
		}
		return c.Complete()
	case "response.failed", "response.incomplete":
		if c.completed {
			return nil
		}
		c.completed = true
		errorPayload := gjson.GetBytes(payload, "response.error")
		if !errorPayload.Exists() {
			errorPayload = gjson.GetBytes(payload, "error")
		}
		if errorPayload.Exists() {
			return [][]byte{sseData(jsonObject(errorPayload.Raw)), []byte("data: [DONE]\n\n")}
		}
	}
	return nil
}

func (c *protocolStreamConverter) chatToResponses(payload []byte) [][]byte {
	c.captureChatMetadata(payload)
	choice := gjson.GetBytes(payload, "choices.0")
	if !choice.Exists() {
		return nil
	}
	if usage := gjson.GetBytes(payload, "usage"); usage.Exists() {
		c.usage = jsonObject(usage.Raw)
	}
	var result [][]byte
	if delta := choice.Get("delta.content").String(); delta != "" {
		result = append(result, c.ensureResponsesTextStarted()...)
		c.outputText.WriteString(delta)
		result = append(result, sseEvent("response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "delta": delta, "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(),
		}))
	}
	choice.Get("delta.tool_calls").ForEach(func(_, value gjson.Result) bool {
		c.sawToolCall = true
		result = append(result, c.ensureResponsesStarted()...)
		function := value.Get("function")
		if function.Get("name").String() != "" {
			result = append(result, sseEvent("response.output_item.added", map[string]any{
				"type": "response.output_item.added", "output_index": 0,
				"item": map[string]any{"id": value.Get("id").String(), "type": "function_call", "status": "in_progress", "call_id": value.Get("id").String(), "name": function.Get("name").String(), "arguments": ""},
			}))
		}
		if arguments := function.Get("arguments").String(); arguments != "" {
			result = append(result, sseEvent("response.function_call_arguments.delta", map[string]any{
				"type": "response.function_call_arguments.delta", "output_index": 0, "item_id": value.Get("id").String(), "call_id": value.Get("id").String(), "delta": arguments,
			}))
		}
		return true
	})
	return result
}

func (c *protocolStreamConverter) completeChatStream() [][]byte {
	finishReason := "stop"
	if c.sawToolCall {
		finishReason = "tool_calls"
	}
	var usage any
	if c.usage != nil {
		usage = responseUsageToChat(c.usage)
	}
	return append(c.chatChunk(map[string]any{}, finishReason, usage), []byte("data: [DONE]\n\n"))
}

func (c *protocolStreamConverter) completeResponsesStream() [][]byte {
	result := c.ensureResponsesTextStarted()
	if c.contentStarted {
		result = append(result,
			sseEvent("response.output_text.done", map[string]any{"type": "response.output_text.done", "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(), "text": c.outputText.String()}),
			sseEvent("response.content_part.done", map[string]any{"type": "response.content_part.done", "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(), "part": map[string]any{"type": "output_text", "text": c.outputText.String()}}),
		)
	}
	response := c.responseObject("completed")
	if c.usage != nil {
		response["usage"] = chatUsageToResponse(c.usage)
	}
	result = append(result,
		sseEvent("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": c.responseOutputItem()}),
		sseEvent("response.completed", map[string]any{"type": "response.completed", "response": response}),
	)
	return result
}

func (c *protocolStreamConverter) ensureChatRole() [][]byte {
	if c.started {
		return nil
	}
	c.started = true
	return c.chatChunk(map[string]any{"role": "assistant"}, nil, nil)
}

func (c *protocolStreamConverter) ensureResponsesStarted() [][]byte {
	if c.started {
		return nil
	}
	c.started = true
	return [][]byte{
		sseEvent("response.created", map[string]any{"type": "response.created", "response": c.responseObject("in_progress")}),
		sseEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": c.responseOutputItem()}),
	}
}

func (c *protocolStreamConverter) ensureResponsesTextStarted() [][]byte {
	result := c.ensureResponsesStarted()
	if c.contentStarted {
		return result
	}
	c.contentStarted = true
	return append(result, sseEvent("response.content_part.added", map[string]any{
		"type": "response.content_part.added", "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(), "part": map[string]any{"type": "output_text", "text": ""},
	}))
}

func (c *protocolStreamConverter) chatChunk(delta map[string]any, finishReason any, usage any) [][]byte {
	chunk := map[string]any{
		"id": c.chatID(), "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	return [][]byte{sseData(chunk)}
}

func (c *protocolStreamConverter) captureResponseMetadata(payload []byte) {
	response := gjson.GetBytes(payload, "response")
	if !response.Exists() {
		return
	}
	if c.id == "" {
		c.id = response.Get("id").String()
	}
	if c.model == "" {
		c.model = response.Get("model").String()
	}
	if created := response.Get("created_at").Int(); created > 0 {
		c.created = created
	}
}

func (c *protocolStreamConverter) captureChatMetadata(payload []byte) {
	if c.id == "" {
		c.id = gjson.GetBytes(payload, "id").String()
	}
	if c.model == "" {
		c.model = gjson.GetBytes(payload, "model").String()
	}
	if created := gjson.GetBytes(payload, "created").Int(); created > 0 {
		c.created = created
	}
}

func (c *protocolStreamConverter) chatID() string {
	if c.id != "" {
		return c.id
	}
	return "chatcmpl-aiferry"
}

func (c *protocolStreamConverter) responseMessageID() string {
	return "msg_aiferry"
}

func (c *protocolStreamConverter) responseOutputItem() map[string]any {
	content := make([]any, 0)
	if c.contentStarted {
		content = append(content, map[string]any{"type": "output_text", "text": c.outputText.String()})
	}
	return map[string]any{"id": c.responseMessageID(), "type": "message", "status": "completed", "role": "assistant", "content": content}
}

func (c *protocolStreamConverter) responseObject(status string) map[string]any {
	id := c.id
	if id == "" {
		id = "resp_aiferry"
	}
	return map[string]any{"id": id, "object": "response", "created_at": c.created, "status": status, "model": c.model, "output": []any{c.responseOutputItem()}}
}

func sseDataPayload(line []byte) (payload []byte, done, valid bool) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return nil, false, false
	}
	value := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if value == "[DONE]" {
		return nil, true, true
	}
	if !json.Valid([]byte(value)) {
		return nil, false, false
	}
	return []byte(value), false, true
}

func sseData(payload any) []byte {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return []byte("data: " + string(encoded) + "\n\n")
}

func sseEvent(event string, payload any) []byte {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return []byte("event: " + event + "\ndata: " + string(encoded) + "\n\n")
}

func jsonObject(raw string) map[string]any {
	value := make(map[string]any)
	_ = json.Unmarshal([]byte(raw), &value)
	return value
}
