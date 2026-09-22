package relay

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func TestAggregateOpenCodeFreeSSEBuildsChatCompletion(t *testing.T) {
	sse := []byte(
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"grok-code-fast-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}` + "\n" +
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}` + "\n" +
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n" +
			"data: [DONE]\n",
	)
	result := aggregateOpenCodeFreeSSE(sse)
	if !json.Valid(result) {
		t.Fatalf("expected valid JSON, got %s", result)
	}
	if object := gjson.GetBytes(result, "object").String(); object != "chat.completion" {
		t.Fatalf("expected chat.completion, got %q", object)
	}
	if model := gjson.GetBytes(result, "model").String(); model != "grok-code-fast-1" {
		t.Fatalf("expected model carried over, got %q", model)
	}
	if content := gjson.GetBytes(result, "choices.0.message.content").String(); content != "Hello" {
		t.Fatalf("expected concatenated content, got %q", content)
	}
	if reason := gjson.GetBytes(result, "choices.0.finish_reason").String(); reason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", reason)
	}
	if total := gjson.GetBytes(result, "usage.total_tokens").Int(); total != 15 {
		t.Fatalf("expected usage carried over, got %d", total)
	}
}

func TestAggregateOpenCodeFreeSSECarriesReasoningAndToolCalls(t *testing.T) {
	sse := []byte(
		`data: {"id":"c2","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"think "}}]}` + "\n" +
			`data: {"id":"c2","choices":[{"index":0,"delta":{"reasoning_content":"more"}}]}` + "\n" +
			`data: {"id":"c2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":"{\"co"}}]}}]}` + "\n" +
			`data: {"id":"c2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"mmand\":\"ls\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n" +
			"data: [DONE]\n",
	)
	result := aggregateOpenCodeFreeSSE(sse)
	if reasoning := gjson.GetBytes(result, "choices.0.message.reasoning_content").String(); reasoning != "think more" {
		t.Fatalf("expected concatenated reasoning, got %q", reasoning)
	}
	call := gjson.GetBytes(result, "choices.0.message.tool_calls.0")
	if name := call.Get("function.name").String(); name != "bash" {
		t.Fatalf("expected tool name bash, got %q", name)
	}
	if args := call.Get("function.arguments").String(); args != `{"command":"ls"}` {
		t.Fatalf("expected merged arguments, got %q", args)
	}
	if id := call.Get("id").String(); id != "call_1" {
		t.Fatalf("expected tool call id, got %q", id)
	}
}

func TestAggregateOpenCodeFreeSSEPassesThroughNonSSE(t *testing.T) {
	raw := []byte(`{"error":{"message":"FreeTierError","type":"invalid_request_error"}}`)
	result := aggregateOpenCodeFreeSSE(raw)
	if string(result) != string(raw) {
		t.Fatalf("expected non-SSE passthrough, got %s", result)
	}
	empty := aggregateOpenCodeFreeSSE([]byte("data: [DONE]\n"))
	if string(empty) != "data: [DONE]\n" {
		t.Fatalf("expected empty stream passthrough, got %s", empty)
	}
}

func TestSplitSSELinesHandlesCRLF(t *testing.T) {
	lines := splitSSELines([]byte("data: a\r\ndata: b\n\ndata: c"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), lines)
	}
	if string(lines[0]) != "data: a" || string(lines[1]) != "data: b" || string(lines[2]) != "data: c" {
		t.Fatalf("unexpected lines: %q", lines)
	}
}
