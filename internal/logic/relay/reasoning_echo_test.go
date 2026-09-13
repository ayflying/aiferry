package relay

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
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
	ids := capture.ToolCallIDs()
	if len(ids) != 1 || ids[0] != "call_1" {
		t.Fatalf("tool call ids = %v, want [call_1]", ids)
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
	reasoning, ids := captureBufferedReasoning("/chat/completions", body)
	if reasoning != "先思考" || len(ids) != 1 || ids[0] != "call_9" {
		t.Fatalf("reasoning=%q ids=%v", reasoning, ids)
	}
	if reasoning, ids = captureBufferedReasoning("/responses", body); reasoning != "" || ids != nil {
		t.Fatalf("非 Chat 上游不应捕获：%q %v", reasoning, ids)
	}
}

func TestPatchReasoningContentFillsMissingToolCallReasoning(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	var looked []string
	patched := patchReasoningContent(body, func(ids []string) string {
		looked = ids
		return "上游原始思考"
	})
	if len(looked) != 1 || looked[0] != "call_1" {
		t.Fatalf("lookup ids = %v, want [call_1]", looked)
	}
	if got := gjson.GetBytes(patched, "messages.1.reasoning_content").String(); got != "上游原始思考" {
		t.Fatalf("assistant 消息未补回思考内容：%s", patched)
	}
	if gjson.GetBytes(patched, "messages.0.reasoning_content").Exists() {
		t.Fatalf("user 消息不应被改写：%s", patched)
	}
}

func TestPatchReasoningContentKeepsExistingReasoning(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","reasoning_content":"客户端自带的","tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) string {
		t.Fatal("已有 reasoning_content 时不应查询存档")
		return ""
	})
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content").String(); got != "客户端自带的" {
		t.Fatalf("客户端自带的思考内容必须保留：%s", patched)
	}
}

func TestPatchReasoningContentIgnoresMessagesWithoutToolCalls(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":"普通回答"},{"role":"assistant","content":"","tool_calls":[]}]}`)
	patched := patchReasoningContent(body, func([]string) string {
		t.Fatal("没有工具调用时不应查询存档")
		return ""
	})
	if string(patched) != string(body) {
		t.Fatalf("请求体应保持不变：%s", patched)
	}
}

func TestPatchReasoningContentLeavesBodyWhenNoArchive(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) string { return "" })
	if string(patched) != string(body) {
		t.Fatalf("没有存档时请求体应保持不变：%s", patched)
	}
}

func TestPatchReasoningContentIgnoresNonChatBody(t *testing.T) {
	for _, body := range []string{`{"input":"hello"}`, `{"messages":"oops"}`, `not json`} {
		patched := patchReasoningContent([]byte(body), func([]string) string {
			t.Fatal("非 Chat 请求体不应查询存档")
			return ""
		})
		if string(patched) != body {
			t.Fatalf("请求体应保持不变：%s", patched)
		}
	}
}

func TestReasoningEchoKeyScopesByAPIKey(t *testing.T) {
	key := reasoningEchoKey(7, "call_1")
	if !strings.HasPrefix(key, reasoningEchoKeyPrefix) {
		t.Fatalf("键应使用独立命名空间：%s", key)
	}
	if key == reasoningEchoKey(8, "call_1") {
		t.Fatal("不同 API 密钥的存档必须隔离")
	}
}
