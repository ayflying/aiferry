package protocol

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

// Responses 的 reasoning 输入项必须回填成 Chat 的 reasoning_content：
// DeepSeek/Kimi 等开启思考的上游在发生过工具调用后要求原样回传，否则直接 400。
func TestResponsesReasoningBackfillsReasoningContent(t *testing.T) {
	body, err := responsesRequestToChat([]byte(`{
  "model":"deepseek-v4-pro",
  "input":[
    {"type":"message","role":"user","content":[{"type":"input_text","text":"查天气"}]},
    {"type":"reasoning","summary":[{"type":"summary_text","text":"需要先查两个城市"}]},
    {"type":"function_call","id":"fc_1","call_id":"call_a","name":"weather","arguments":"{\"city\":\"wuhan\"}"},
    {"type":"function_call_output","call_id":"call_a","output":"晴"}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "messages.1.reasoning_content").String(); actual != "需要先查两个城市" {
		t.Fatalf("reasoning_content = %q, body=%s", actual, body)
	}
	// 只写标准字段：聚合方言 reasoning 在 DeepSeek/GLM 系会被拒绝。
	if gjson.GetBytes(body, "messages.1.reasoning").Exists() {
		t.Fatalf("reasoning must not be written, body=%s", body)
	}
}

// reasoning 的 content 形态同样要认：部分客户端回传完整思考链而非 summary。
func TestResponsesReasoningAcceptsContentParts(t *testing.T) {
	body, err := responsesRequestToChat([]byte(`{
  "model":"deepseek-v4-pro",
  "input":[
    {"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
    {"type":"reasoning","content":[{"type":"reasoning_text","text":"完整思考链"}]},
    {"type":"function_call","id":"fc_1","call_id":"call_a","name":"read","arguments":"{}"},
    {"type":"function_call_output","call_id":"call_a","output":"ok"}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "messages.1.reasoning_content").String(); actual != "完整思考链" {
		t.Fatalf("reasoning_content = %q, body=%s", actual, body)
	}
}

// 空 reasoning 不写字段，避免给非思考模型平白加一个空 reasoning_content。
func TestResponsesEmptyReasoningWritesNothing(t *testing.T) {
	body, err := responsesRequestToChat([]byte(`{
  "model":"deepseek-v4-pro",
  "input":[
    {"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
    {"type":"reasoning","summary":[]},
    {"type":"function_call","id":"fc_1","call_id":"call_a","name":"read","arguments":"{}"},
    {"type":"function_call_output","call_id":"call_a","output":"ok"}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "messages.1.reasoning_content").Exists() {
		t.Fatalf("empty reasoning must not write the field, body=%s", body)
	}
}

// 上游 call_id 缺失时回退条目 id，避免工具结果对不上调用。
func TestResponsesToolCallFallsBackToItemID(t *testing.T) {
	converted := responsesResponseToChat([]byte(`{
  "id":"resp_1","object":"response","status":"completed","model":"m","created_at":1,
  "output":[{"type":"function_call","id":"fc_only","name":"read","arguments":"{}"}]
}`))
	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatal(err)
	}
	choices := parsed["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	calls := message["tool_calls"].([]any)
	if id := calls[0].(map[string]any)["id"]; id != "fc_only" {
		t.Fatalf("tool call id = %v, want fallback to item id, body=%s", id, converted)
	}
}

// Chat 侧工具调用缺 id 时补稳定 id，否则 Responses 客户端无法回填结果。
func TestChatToolCallsToResponsesFillsMissingID(t *testing.T) {
	calls := chatToolCallsToResponses([]any{map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "read", "arguments": "{}"},
	}})
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	call := calls[0].(map[string]any)
	if call["id"] == "" || call["id"] == nil {
		t.Fatalf("id must not be empty: %+v", call)
	}
	if call["id"] != call["call_id"] {
		t.Fatalf("id and call_id must match: %+v", call)
	}
}
