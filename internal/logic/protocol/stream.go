package protocol

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type StreamConverter struct {
	plan              Plan
	id                string
	model             string
	created           int64
	started           bool
	chatRoleSent      bool
	anthropicFinished bool
	contentStarted    bool
	completed         bool
	sawToolCall       bool
	stopReason        string
	outputText        strings.Builder
	usage             map[string]any
	chatToolIndexes   map[string]int
	chatToolCount     int
	chatToolNameSent  map[string]bool
	chatToolArgs      map[int]*strings.Builder
	chatToolArgsSent  map[int]int
	responsesTools    map[int]*responsesToolState
	responsesToolSeq  int
}

func NewStreamConverter(plan Plan) *StreamConverter {
	return &StreamConverter{
		plan:             plan,
		created:          time.Now().Unix(),
		chatToolIndexes:  make(map[string]int),
		chatToolNameSent: make(map[string]bool),
		chatToolArgs:     make(map[int]*strings.Builder),
		chatToolArgsSent: make(map[int]int),
		responsesTools:   make(map[int]*responsesToolState),
	}
}

func (c *StreamConverter) Transform(line []byte) [][]byte {
	payload, done, valid := sseDataPayload(line)
	if !valid {
		return nil
	}
	if done {
		return c.Complete()
	}
	switch c.plan.Conversion() {
	case chatToResponsesConversion:
		return c.responsesToChat(payload)
	case responsesToChatConversion:
		return c.chatToResponses(payload)
	case chatToAnthropicConversion:
		return c.anthropicToChat(payload)
	case responsesToAnthropicConversion:
		return c.anthropicToResponses(payload)
	default:
		return [][]byte{line}
	}
}

func (c *StreamConverter) Complete() [][]byte {
	switch c.plan.Conversion() {
	case chatToAnthropicConversion, responsesToAnthropicConversion:
		// anthropic 方向的幂等由 completeChunks 的 anthropFinished 保证：
		// message_stop/error 已把 completed 置位，这里不能因它短路——
		// responses 客户端方向还欠一段合成收尾（completed 事件）。
	default:
		if c.completed {
			return nil
		}
		c.completed = true
	}
	switch c.plan.Conversion() {
	case chatToResponsesConversion:
		return c.completeChatStream()
	case responsesToChatConversion:
		return c.completeResponsesStream()
	case chatToAnthropicConversion:
		return c.completeAnthropicChatStream()
	case responsesToAnthropicConversion:
		return c.completeAnthropicResponsesStream()
	default:
		return nil
	}
}

func (c *StreamConverter) responsesToChat(payload []byte) [][]byte {
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
		itemID := item.Get("id").String()
		callID := item.Get("call_id").String()
		index := c.chatToolIndex(itemID, callID)
		var result [][]byte
		// Chat Completions 的 function.name 是增量字段。部分 Responses 上游会
		// 重复发送 output_item.added；同一调用若再次下发完整名称，客户端会
		// 按 delta 拼成 PowerShellPowerShell。每个 call 只发送一次名称。
		nameKey := stringOr(itemID, callID)
		if !c.chatToolNameSent[nameKey] {
			c.chatToolNameSent[nameKey] = true
			c.sawToolCall = true
			result = append(c.ensureChatRole(), c.chatChunk(map[string]any{"tool_calls": []any{map[string]any{
				"index":    index,
				"id":       stringOr(callID, itemID),
				"type":     "function",
				"function": map[string]any{"name": item.Get("name").String()},
			}}}, nil, nil)...)
		}
		// 部分上游把完整参数直接放在 added 的 item.arguments 里、不随流式
		// delta 下发。只转发名称会丢掉全部参数，客户端（如 DSH）拿到的
		// tool_call 参数为空，校验直接报 missing required property。收进
		// 累计值并只补发未下发的尾部，重复的 added 不会让参数翻倍。
		if tail := c.chatToolRecordArgs(index, item.Get("arguments").String()); tail != "" {
			c.sawToolCall = true
			result = append(result, c.chatToolArgsChunk(index, stringOr(callID, itemID), tail)...)
		}
		return result
	case "response.function_call_arguments.delta":
		delta := gjson.GetBytes(payload, "delta").String()
		if delta == "" {
			return nil
		}
		itemID := gjson.GetBytes(payload, "item_id").String()
		callID := gjson.GetBytes(payload, "call_id").String()
		index := c.chatToolIndex(itemID, callID)
		c.sawToolCall = true
		// 走累计值 + 水位线转发：正常流式下尾部恰好等于本次 delta；上游
		// 重复或乱序时，已下发过的内容不会重复转发。
		tail := c.chatToolAppendArgs(index, delta)
		if tail == "" {
			return nil
		}
		return append(c.ensureChatRole(), c.chatToolArgsChunk(index, stringOr(callID, itemID), tail)...)
	case "response.function_call_arguments.done":
		// done 携带该调用的参数权威全量。部分上游不流式发 delta（或只在
		// added 里给一次性全量），done 是参数到达客户端的最后机会；按水位线
		// 只补差额，正常流式下累计值与权威全量一致，不会产生重复。
		itemID := gjson.GetBytes(payload, "item_id").String()
		callID := gjson.GetBytes(payload, "call_id").String()
		if itemID == "" && callID == "" {
			return nil
		}
		index := c.chatToolIndex(itemID, callID)
		tail := c.chatToolRecordArgs(index, gjson.GetBytes(payload, "arguments").String())
		if tail == "" {
			return nil
		}
		c.sawToolCall = true
		return append(c.ensureChatRole(), c.chatToolArgsChunk(index, stringOr(callID, itemID), tail)...)
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

func (c *StreamConverter) chatToResponses(payload []byte) [][]byte {
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
		tool := c.responsesToolFor(value)
		function := value.Get("function")
		if name := function.Get("name").String(); name != "" {
			tool.name = name
		}
		if id := value.Get("id").String(); id != "" {
			tool.callID = id
		}
		if arguments := function.Get("arguments").String(); arguments != "" {
			tool.args.WriteString(arguments)
		}
		// output_item.added 只在名称首次确定时申报一次；申报前到达的参数先
		// 进缓冲，申报后以一条 delta 补发（上游首片即带全量参数时，同一批
		// 输出 added + delta，参数不会被计入两次）。
		if !tool.announced && tool.name != "" {
			result = append(result, c.ensureResponsesStarted()...)
			tool.announced = true
			result = append(result, responsesToolAnnounceItem(tool))
		}
		if tool.announced {
			if pending := responsesToolPendingArgs(tool); pending != "" {
				result = append(result, c.responsesToolArgsDelta(tool, pending))
			}
		}
		return true
	})
	return result
}

func (c *StreamConverter) completeChatStream() [][]byte {
	// 收尾兜底：把尚未下发的参数尾部补发出去（参数只出现在 done 事件或
	// 单个分片里时，这是它们到达客户端的最后机会，对应 new-api 的
	// flushAllPendingTools），再发 finish_reason。
	result := c.chatToolFlushPendingArgs()
	finishReason := "stop"
	if c.sawToolCall {
		finishReason = "tool_calls"
	}
	var usage any
	if c.usage != nil {
		usage = responseUsageToChat(c.usage)
	}
	result = append(result, c.chatChunk(map[string]any{}, finishReason, usage)...)
	return append(result, []byte("data: [DONE]\n\n"))
}

func (c *StreamConverter) completeResponsesStream() [][]byte {
	result := c.ensureResponsesTextStarted()
	if c.contentStarted {
		result = append(result,
			sseEvent("response.output_text.done", map[string]any{"type": "response.output_text.done", "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(), "text": c.outputText.String()}),
			sseEvent("response.content_part.done", map[string]any{"type": "response.content_part.done", "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(), "part": map[string]any{"type": "output_text", "text": c.outputText.String()}}),
		)
	}
	// 收尾逐调用落定（sub2api 的做法）：名称始终没到的调用也强制申报
	// output_item.added，随后每个调用补 function_call_arguments.done 与
	// output_item.done——Codex 类客户端只在两者齐备后才把调用落定，缺了
	// 会话会卡住；参数空缺以 "{}" 收口，避免客户端解析残缺 JSON。
	for _, tool := range c.sortedResponsesTools() {
		if !tool.announced {
			result = append(result, c.ensureResponsesStarted()...)
			tool.announced = true
			result = append(result, responsesToolAnnounceItem(tool))
		}
		args := tool.finalizedArgs()
		result = append(result,
			sseEvent("response.function_call_arguments.done", map[string]any{
				"type": "response.function_call_arguments.done", "output_index": tool.outputIndex,
				"item_id": tool.itemID, "call_id": tool.callID, "arguments": args,
			}),
			sseEvent("response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": tool.outputIndex,
				"item": map[string]any{
					"id": tool.itemID, "type": "function_call", "status": "completed",
					"call_id": tool.callID, "name": tool.name, "arguments": args,
				},
			}),
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

func (c *StreamConverter) ensureChatRole() [][]byte {
	if c.started {
		return nil
	}
	c.started = true
	return c.chatChunk(map[string]any{"role": "assistant"}, nil, nil)
}

func (c *StreamConverter) ensureResponsesStarted() [][]byte {
	if c.started {
		return nil
	}
	c.started = true
	return [][]byte{
		sseEvent("response.created", map[string]any{"type": "response.created", "response": c.responseObject("in_progress")}),
		sseEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": c.responseOutputItem()}),
	}
}

func (c *StreamConverter) ensureResponsesTextStarted() [][]byte {
	result := c.ensureResponsesStarted()
	if c.contentStarted {
		return result
	}
	c.contentStarted = true
	return append(result, sseEvent("response.content_part.added", map[string]any{
		"type": "response.content_part.added", "output_index": 0, "content_index": 0, "item_id": c.responseMessageID(), "part": map[string]any{"type": "output_text", "text": ""},
	}))
}

func (c *StreamConverter) chatChunk(delta map[string]any, finishReason any, usage any) [][]byte {
	chunk := map[string]any{
		"id": c.chatID(), "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	return [][]byte{sseData(chunk)}
}

func (c *StreamConverter) captureResponseMetadata(payload []byte) {
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

func (c *StreamConverter) captureChatMetadata(payload []byte) {
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

// chatToolIndex 为每次工具调用分配 Chat Completions 的 tool_calls 下标。传入的
// 多个键（item.id 与 call_id）会作为别名指向同一下标，任意一个命中即复用。
//
// 两个约束必须同时满足，否则客户端会把并行工具调用读错：
//   - 下标要从 0 起连续分配，不能照搬上游 output_index——Responses 的
//     output_index 把 message、reasoning 等输出项一并计入，透传会留下空洞。
//   - output_item.added 携带 item.id + call_id，而部分上游的
//     function_call_arguments.delta 只带 item_id、不带 call_id。若只认单一键，
//     同一次调用的「名称」与「参数」会被算成两次调用、落到两个下标上，
//     客户端表现为多个工具的参数拼成一个 JSON 且工具名对不上（Tool not found）。
func (c *StreamConverter) chatToolIndex(keys ...string) int {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if index, exists := c.chatToolIndexes[key]; exists {
			return index
		}
	}
	index := c.chatToolCount
	c.chatToolCount++
	for _, key := range keys {
		if key != "" {
			c.chatToolIndexes[key] = index
		}
	}
	return index
}

func (c *StreamConverter) chatID() string {
	if c.id != "" {
		return c.id
	}
	return "chatcmpl-aiferry"
}

func (c *StreamConverter) responseMessageID() string {
	return "msg_aiferry"
}

func (c *StreamConverter) responseOutputItem() map[string]any {
	content := make([]any, 0)
	if c.contentStarted {
		content = append(content, map[string]any{"type": "output_text", "text": c.outputText.String()})
	}
	return map[string]any{"id": c.responseMessageID(), "type": "message", "status": "completed", "role": "assistant", "content": content}
}

func (c *StreamConverter) responseObject(status string) map[string]any {
	id := c.id
	if id == "" {
		id = "resp_aiferry"
	}
	// 已申报的工具调用并入 response.output（按槽位顺序排在消息项之后）；
	// 未申报的不出现，避免 response.created 的 output 提前暴露未成形调用。
	output := []any{c.responseOutputItem()}
	for _, tool := range c.sortedResponsesTools() {
		if !tool.announced {
			continue
		}
		output = append(output, map[string]any{
			"id": tool.itemID, "type": "function_call", "status": "completed",
			"call_id": tool.callID, "name": tool.name, "arguments": tool.finalizedArgs(),
		})
	}
	return map[string]any{"id": id, "object": "response", "created_at": c.created, "status": status, "model": c.model, "output": output}
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
