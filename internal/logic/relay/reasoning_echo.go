package relay

import (
	"bytes"

	"strings"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// 仅捕获上游思考内容供响应记录使用，不存档、不回填请求历史。
const (
	reasoningFieldStandard = "reasoning_content"
	reasoningFieldCompact  = "reasoning"
)

// reasoningFromContainer 按优先级从 message/delta 容器里读取思考内容，并回报实际命中的
// 字段名，供回传时沿用。返回空串表示该容器没有思考内容。
func reasoningFromContainer(container gjson.Result) (string, string) {
	if text := container.Get(reasoningFieldStandard).String(); text != "" {
		return text, reasoningFieldStandard
	}
	if text := container.Get(reasoningFieldCompact).String(); text != "" {
		return text, reasoningFieldCompact
	}
	return "", ""
}

// reasoningCapture 从上游 Chat Completions 响应里累积思考内容与工具调用 id。
// 流式场景必须在响应改写（normalizeSSELine）之前观察：渠道开启 ReasoningToContent 时，
// 改写会把 reasoning_content 合并进 content 并删除原字段。
type reasoningCapture struct {
	endpoint  string
	reasoning strings.Builder
	field     string
	toolCalls []string
	seen      map[string]struct{}
}

func newReasoningCapture(endpoint string) *reasoningCapture {
	return &reasoningCapture{endpoint: endpoint, seen: make(map[string]struct{})}
}

// Observe 处理一行上游 SSE。先做子串预筛，避免为无关行解析 JSON。
func (c *reasoningCapture) Observe(line []byte) {
	if c == nil || c.endpoint != protocol.ChatCompletionsEndpoint {
		return
	}
	// "reasoning" 是 "reasoning_content" 的子串，一个判断即可覆盖两种方言。
	if !bytes.Contains(line, []byte(reasoningFieldCompact)) && !bytes.Contains(line, []byte("tool_calls")) {
		return
	}
	payload, done, valid := relaySSEDataPayload(line)
	if !valid || done {
		return
	}
	delta := gjson.GetBytes(payload, "choices.0.delta")
	if text, field := reasoningFromContainer(delta); text != "" {
		if c.field == "" {
			c.field = field
		}
		c.reasoning.WriteString(text)
	}
	for _, value := range delta.Get("tool_calls").Array() {
		c.addToolCall(value.Get("id").String())
	}
}

func (c *reasoningCapture) Reasoning() string {
	if c == nil {
		return ""
	}
	return c.reasoning.String()
}

// Field 返回上游实际使用的思考内容字段名，未捕获到时按标准字段名处理。
func (c *reasoningCapture) Field() string {
	if c == nil || c.field == "" {
		return reasoningFieldStandard
	}
	return c.field
}

func (c *reasoningCapture) ToolCallIDs() []string {
	if c == nil {
		return nil
	}
	return c.toolCalls
}

func (c *reasoningCapture) addToolCall(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, exists := c.seen[id]; exists {
		return
	}
	c.seen[id] = struct{}{}
	c.toolCalls = append(c.toolCalls, id)
}

// captureBufferedReasoning 读取非流式 Chat Completions 响应里的思考内容（含字段名）与工具调用 id。
// 与流式一样，必须在 normalizeResponseBody 之前调用。
func captureBufferedReasoning(endpoint string, body []byte) (string, string, []string) {
	if endpoint != protocol.ChatCompletionsEndpoint {
		return "", "", nil
	}
	message := gjson.GetBytes(body, "choices.0.message")
	if !message.Exists() {
		return "", "", nil
	}
	ids := make([]string, 0, 1)
	for _, value := range message.Get("tool_calls").Array() {
		if id := strings.TrimSpace(value.Get("id").String()); id != "" {
			ids = append(ids, id)
		}
	}
	text, field := reasoningFromContainer(message)
	return text, field, ids
}

func chatToolCallIDs(message gjson.Result) []string {
	calls := message.Get("tool_calls")
	if !calls.IsArray() {
		return nil
	}
	ids := make([]string, 0, len(calls.Array()))
	for _, call := range calls.Array() {
		if id := strings.TrimSpace(call.Get("id").String()); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
