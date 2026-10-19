package relay

import (
	"testing"
)

func TestReasoningCaptureCollectsStreamingReasoningAndToolCalls(t *testing.T) {
	capture := newReasoningCapture("/chat/completions")
	for _, line := range []string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"先看","content":""}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"目录结构"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Bash","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"{\"cmd\":\"ls\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	} {
		capture.Observe([]byte(line + "\n"))
	}
	if got := capture.Reasoning(); got != "先看目录结构" {
		t.Fatalf("reasoning = %q, want %q", got, "先看目录结构")
	}
	if got := capture.Field(); got != reasoningFieldStandard {
		t.Fatalf("field = %q, want %q", got, reasoningFieldStandard)
	}
	ids := capture.ToolCallIDs()
	if len(ids) != 1 || ids[0] != "call_1" {
		t.Fatalf("tool call ids = %v, want [call_1]", ids)
	}
}

// OpenCode 系聚合端点（含 opencode_go）用 reasoning 而不是 reasoning_content，
// 捕获必须认这个方言，并把它记为回传字段名。
func TestReasoningCaptureAcceptsCompactReasoningField(t *testing.T) {
	capture := newReasoningCapture("/chat/completions")
	for _, line := range []string{
		`data: {"choices":[{"delta":{"reasoning":"先分析"}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"再动手"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_7","function":{"name":"Bash","arguments":"{}"}}]}}]}`,
		`data: [DONE]`,
	} {
		capture.Observe([]byte(line + "\n"))
	}
	if got := capture.Reasoning(); got != "先分析再动手" {
		t.Fatalf("reasoning = %q", got)
	}
	if got := capture.Field(); got != reasoningFieldCompact {
		t.Fatalf("field = %q, want %q", got, reasoningFieldCompact)
	}
	if ids := capture.ToolCallIDs(); len(ids) != 1 || ids[0] != "call_7" {
		t.Fatalf("tool call ids = %v", ids)
	}
}

// 未捕获到内容时按标准字段名兜底，避免存出空字段名。
func TestReasoningCaptureFieldDefaultsToStandard(t *testing.T) {
	capture := newReasoningCapture("/chat/completions")
	if got := capture.Field(); got != reasoningFieldStandard {
		t.Fatalf("field = %q, want %q", got, reasoningFieldStandard)
	}
}

func TestReasoningCaptureIgnoresNonChatEndpoint(t *testing.T) {
	capture := newReasoningCapture("/responses")
	capture.Observe([]byte(`data: {"choices":[{"delta":{"reasoning_content":"思考","tool_calls":[{"id":"call_1"}]}}]}` + "\n"))
	if capture.Reasoning() != "" || len(capture.ToolCallIDs()) != 0 {
		t.Fatalf("非 Chat 上游不应捕获：%q %v", capture.Reasoning(), capture.ToolCallIDs())
	}
}

func TestCaptureBufferedReasoning(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"先思考","tool_calls":[{"id":"call_9","type":"function","function":{"name":"Read","arguments":"{}"}}]}}]}`)
	reasoning, field, ids := captureBufferedReasoning("/chat/completions", body)
	if reasoning != "先思考" || field != reasoningFieldStandard || len(ids) != 1 || ids[0] != "call_9" {
		t.Fatalf("reasoning=%q field=%q ids=%v", reasoning, field, ids)
	}

	compact := []byte(`{"choices":[{"message":{"role":"assistant","reasoning":"聚合方言","tool_calls":[{"id":"call_3"}]}}]}`)
	reasoning, field, ids = captureBufferedReasoning("/chat/completions", compact)
	if reasoning != "聚合方言" || field != reasoningFieldCompact || len(ids) != 1 || ids[0] != "call_3" {
		t.Fatalf("reasoning=%q field=%q ids=%v", reasoning, field, ids)
	}

	if reasoning, field, ids = captureBufferedReasoning("/responses", body); reasoning != "" || field != "" || ids != nil {
		t.Fatalf("非 Chat 上游不应捕获：%q %q %v", reasoning, field, ids)
	}
}
