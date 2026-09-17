package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestChatRequestToAnthropicMapsMessages(t *testing.T) {
	body := []byte(`{
		"model": "union-alpha",
		"stream": true,
		"max_completion_tokens": 128,
		"stop": "END",
		"messages": [
			{"role": "system", "content": "你是测试助手"},
			{"role": "user", "content": "你好"},
			{"role": "assistant", "content": "你好呀", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"武汉\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "晴"},
			{"role": "user", "content": "谢谢"}
		]
	}`)
	converted, err := chatRequestToAnthropic(body)
	if err != nil {
		t.Fatalf("chatRequestToAnthropic error = %v", err)
	}
	if got := gjson.GetBytes(converted, "system").String(); got != "你是测试助手" {
		t.Fatalf("system = %q", got)
	}
	if got := gjson.GetBytes(converted, "max_tokens").Int(); got != 128 {
		t.Fatalf("max_tokens = %d", got)
	}
	if got := gjson.GetBytes(converted, "stop_sequences.0").String(); got != "END" {
		t.Fatalf("stop_sequences[0] = %q", got)
	}
	messages := gjson.GetBytes(converted, "messages").Array()
	// assistant(tool_calls) 与 tool 结果都在，但相邻 user「谢谢」必须与
	// role=tool 转出的 user 消息合并，保证 user/assistant 交替。
	if len(messages) != 3 {
		t.Fatalf("messages len = %d, want 3: %s", len(messages), converted)
	}
	if messages[1].Get("role").String() != "assistant" ||
		messages[1].Get("content.0.type").String() != "text" ||
		messages[1].Get("content.1.type").String() != "tool_use" ||
		messages[1].Get("content.1.input.city").String() != "武汉" {
		t.Fatalf("assistant message wrong: %s", messages[1].Raw)
	}
	if messages[2].Get("role").String() != "user" ||
		messages[2].Get("content.0.type").String() != "tool_result" ||
		messages[2].Get("content.0.tool_use_id").String() != "call_1" ||
		messages[2].Get("content.1.text").String() != "谢谢" {
		t.Fatalf("merged user message wrong: %s", messages[2].Raw)
	}
}

func TestChatRequestToAnthropicDefaultsMaxTokens(t *testing.T) {
	converted, err := chatRequestToAnthropic([]byte(`{"model":"union-alpha","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got := gjson.GetBytes(converted, "max_tokens").Int(); got != anthropicDefaultMaxTokens {
		t.Fatalf("max_tokens = %d, want %d", got, anthropicDefaultMaxTokens)
	}
}

func TestChatRequestToAnthropicMapsTools(t *testing.T) {
	body := []byte(`{
		"model": "claude-x",
		"tool_choice": {"type": "function", "function": {"name": "lookup"}},
		"tools": [{"type": "function", "function": {"name": "lookup", "description": "查询", "parameters": {"type": "object", "properties": {}}}}],
		"messages": [{"role": "user", "content": "查一下"}]
	}`)
	converted, err := chatRequestToAnthropic(body)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got := gjson.GetBytes(converted, "tool_choice.type").String(); got != "tool" {
		t.Fatalf("tool_choice.type = %q", got)
	}
	if got := gjson.GetBytes(converted, "tools.0.input_schema.type").String(); got != "object" {
		t.Fatalf("input_schema.type = %q", got)
	}
	if got := gjson.GetBytes(converted, "tools.0.name").String(); got != "lookup" {
		t.Fatalf("tools.0.name = %q", got)
	}

	none, err := chatRequestToAnthropic([]byte(`{"model":"claude-x","tool_choice":"none","tools":[{"type":"function","function":{"name":"lookup"}}],"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if gjson.GetBytes(none, "tools").Exists() || gjson.GetBytes(none, "tool_choice").Exists() {
		t.Fatalf("tool_choice=none 应丢弃工具定义: %s", none)
	}
}

func TestChatRequestToAnthropicMapsImages(t *testing.T) {
	body := []byte(`{
		"model": "claude-x",
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "看图"},
			{"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}},
			{"type": "image_url", "image_url": {"url": "https://example.com/a.png"}}
		]}]
	}`)
	converted, err := chatRequestToAnthropic(body)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	content := gjson.GetBytes(converted, "messages.0.content").Array()
	if len(content) != 3 {
		t.Fatalf("content len = %d: %s", len(content), converted)
	}
	if content[1].Get("source.type").String() != "base64" ||
		content[1].Get("source.media_type").String() != "image/png" ||
		content[1].Get("source.data").String() != "AAAA" {
		t.Fatalf("base64 image wrong: %s", content[1].Raw)
	}
	if content[2].Get("source.type").String() != "url" {
		t.Fatalf("url image wrong: %s", content[2].Raw)
	}
}

func TestAnthropicResponseToChat(t *testing.T) {
	body := []byte(`{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "union-alpha",
		"content": [
			{"type": "thinking", "thinking": "想一想"},
			{"type": "text", "text": "答案是 42"},
			{"type": "tool_use", "id": "toolu_1", "name": "calc", "input": {"x": 1}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 4}
	}`)
	converted := anthropicResponseToChat(body)
	if got := gjson.GetBytes(converted, "choices.0.message.content").String(); got != "答案是 42" {
		t.Fatalf("content = %q", got)
	}
	if got := gjson.GetBytes(converted, "choices.0.message.reasoning_content").String(); got != "想一想" {
		t.Fatalf("reasoning_content = %q", got)
	}
	if got := gjson.GetBytes(converted, "choices.0.message.tool_calls.0.function.name").String(); got != "calc" {
		t.Fatalf("tool name = %q", got)
	}
	if got := gjson.GetBytes(converted, "choices.0.message.tool_calls.0.function.arguments").String(); got != `{"x":1}` {
		t.Fatalf("arguments = %q", got)
	}
	if got := gjson.GetBytes(converted, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("finish_reason = %q", got)
	}
	if got := gjson.GetBytes(converted, "usage.prompt_tokens").Int(); got != 10 {
		t.Fatalf("prompt_tokens = %d", got)
	}
	if got := gjson.GetBytes(converted, "usage.prompt_tokens_details.cached_tokens").Int(); got != 4 {
		t.Fatalf("cached_tokens = %d", got)
	}
}

func TestAnthropicFinishReason(t *testing.T) {
	cases := map[[2]any]string{
		{"end_turn", false}:   "stop",
		{"stop_sequence", false}: "stop",
		{"max_tokens", false}: "length",
		{"refusal", false}:    "content_filter",
		{"end_turn", true}:    "tool_calls",
		{"", false}:           "stop",
	}
	for input, want := range cases {
		stop, _ := input[0].(string)
		hasTool, _ := input[1].(bool)
		if got := anthropicFinishReason(stop, hasTool); got != want {
			t.Fatalf("anthropicFinishReason(%q,%v) = %q, want %q", stop, hasTool, got, want)
		}
	}
}

func TestPreferredAnthropicPlan(t *testing.T) {
	chat := PreferredAnthropicPlan(ChatCompletionsEndpoint, "https://opencode.ai/zen/v1/messages")
	if chat.UpstreamEndpoint() != "https://opencode.ai/zen/v1/messages" || chat.Conversion() != chatToAnthropicConversion {
		t.Fatalf("chat plan = %+v", chat)
	}
	responses := PreferredAnthropicPlan(ResponsesEndpoint, "/v1/messages")
	if responses.Conversion() != responsesToAnthropicConversion {
		t.Fatalf("responses plan = %+v", responses)
	}
	// Responses 客户端请求要能一路转成 Anthropic 载荷。
	converted, err := responses.ConvertRequest([]byte(`{"model":"union-alpha","input":"hi","max_output_tokens":16}`))
	if err != nil {
		t.Fatalf("ConvertRequest error = %v", err)
	}
	if gjson.GetBytes(converted, "messages.0.role").String() != "user" || gjson.GetBytes(converted, "max_tokens").Int() != 16 {
		t.Fatalf("converted request wrong: %s", converted)
	}
	// 非流式响应要经 anthropic→chat→responses 两级还原。
	response := responses.ConvertResponse([]byte(`{
		"id":"msg_2","type":"message","role":"assistant","model":"union-alpha",
		"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":2}
	}`))
	if gjson.GetBytes(response, "object").String() != "response" ||
		gjson.GetBytes(response, "output.0.content.0.text").String() != "ok" {
		t.Fatalf("responses conversion wrong: %s", response)
	}
}

func TestAnthropicStreamToChat(t *testing.T) {
	converter := NewStreamConverter(PreferredAnthropicPlan(ChatCompletionsEndpoint, "/v1/messages"))
	var output strings.Builder
	events := []string{
		`{"type":"message_start","message":{"id":"msg_9","model":"union-alpha","usage":{"input_tokens":7}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"呀"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		for _, line := range converter.Transform([]byte("data: " + event + "\n\n")) {
			output.Write(line)
		}
	}
	text := output.String()
	if !strings.Contains(text, `"role":"assistant"`) {
		t.Fatalf("missing role chunk: %s", text)
	}
	// 文本增量逐事件分帧（与 OpenAI chat 流一致），客户端负责拼接；
	// 这里断言两个增量各出现一次即可。
	if strings.Count(text, "你好") != 1 || strings.Count(text, "呀") != 1 {
		t.Fatalf("text delta wrong: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"stop"`) || !strings.Contains(text, "[DONE]") {
		t.Fatalf("missing finish/DONE: %s", text)
	}
	if !strings.Contains(text, `"prompt_tokens":7`) || !strings.Contains(text, `"completion_tokens":3`) {
		t.Fatalf("missing usage: %s", text)
	}
}

func TestAnthropicStreamToChatTools(t *testing.T) {
	converter := NewStreamConverter(PreferredAnthropicPlan(ChatCompletionsEndpoint, "/v1/messages"))
	var output strings.Builder
	events := []string{
		`{"type":"message_start","message":{"id":"msg_t","model":"claude-x","usage":{"input_tokens":5}}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_9","name":"calc","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":6}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		for _, line := range converter.Transform([]byte("data: " + event + "\n\n")) {
			output.Write(line)
		}
	}
	text := output.String()
	if !strings.Contains(text, `"name":"calc"`) {
		t.Fatalf("missing tool name: %s", text)
	}
	// 工具参数按 partial_json 分片下发（OpenAI 流同样如此），客户端累积；
	// 断言两个分片都到达即可，不要求拼成完整 JSON 字符串。
	if !strings.Contains(text, `{"arguments":"{\"x\":"}`) || !strings.Contains(text, `{"arguments":"1}"`) {
		t.Fatalf("arguments chunks wrong: %s", text)
	}
	if !strings.Contains(text, `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing tool_calls finish: %s", text)
	}
	// EOF 兜底路径不得重复发结束帧。
	if extra := converter.Complete(); extra != nil {
		t.Fatalf("Complete after message_stop should be empty: %s", extra)
	}
}

func TestAnthropicStreamToResponses(t *testing.T) {
	converter := NewStreamConverter(PreferredAnthropicPlan(ResponsesEndpoint, "/v1/messages"))
	var output strings.Builder
	events := []string{
		`{"type":"message_start","message":{"id":"msg_r","model":"union-alpha","usage":{"input_tokens":4}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		for _, line := range converter.Transform([]byte("data: " + event + "\n\n")) {
			output.Write(line)
		}
	}
	for _, line := range converter.Complete() {
		output.Write(line)
	}
	text := output.String()
	for _, marker := range []string{"response.created", "response.output_text.delta", "response.completed"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("missing %s: %s", marker, text)
		}
	}
	// completed 事件的 usage 要带上游真实 token 数（经 chat 中转不丢）。
	var usageOK bool
	for _, frame := range strings.Split(text, "\n\n") {
		if !strings.Contains(frame, "response.completed") {
			continue
		}
		for _, line := range strings.Split(frame, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if gjson.Get(payload, "response.usage.input_tokens").Int() == 4 &&
				gjson.Get(payload, "response.usage.output_tokens").Int() == 1 {
				usageOK = true
			}
		}
	}
	if !usageOK {
		t.Fatalf("usage not carried into completed event: %s", text)
	}
}

func TestChatRequestToAnthropicRejectsBadBody(t *testing.T) {
	if _, err := chatRequestToAnthropic([]byte("not-json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestAnthropicResponseToChatPassesThroughInvalid(t *testing.T) {
	if got := anthropicResponseToChat([]byte("not-json")); string(got) != "not-json" {
		t.Fatalf("invalid body should pass through: %s", got)
	}
}

func TestAnthropicUsageToChatSums(t *testing.T) {
	usage := anthropicUsageToChat(map[string]any{"input_tokens": float64(3), "output_tokens": float64(4)})
	if usage["total_tokens"] != 7 {
		t.Fatalf("total_tokens = %v", usage["total_tokens"])
	}
	encoded, _ := json.Marshal(usage)
	if !strings.Contains(string(encoded), `"prompt_tokens":3`) {
		t.Fatalf("usage wrong: %s", encoded)
	}
}
