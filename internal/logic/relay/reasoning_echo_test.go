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

func TestPatchReasoningContentFillsMissingToolCallReasoning(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	var looked []string
	patched := patchReasoningContent(body, func(ids []string) storedReasoning {
		looked = ids
		return storedReasoning{Field: reasoningFieldStandard, Text: "上游原始思考"}
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

// 无论存档记录的是哪种方言，回填一律写标准字段 reasoning_content。
// 实测 reasoning 是「有毒字段」：GLM 与 DeepSeek 见到它就 400，不能写。
func TestPatchReasoningContentNormalizesDialectToStandardField(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning {
		return storedReasoning{Field: reasoningFieldCompact, Text: "聚合端点的思考"}
	})
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content").String(); got != "聚合端点的思考" {
		t.Fatalf("必须写标准字段：%s", patched)
	}
	if gjson.GetBytes(patched, "messages.0.reasoning").Exists() {
		t.Fatalf("不得写入有毒字段 reasoning：%s", patched)
	}
}

// 客户端按聚合方言带回的历史必须被规整：内容转写到标准字段，reasoning 删除。
func TestPatchReasoningContentMovesCompactDialectToStandardField(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","reasoning":"客户端自带的","tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning {
		t.Fatal("客户端已带思考内容时不应查询存档")
		return storedReasoning{}
	})
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content").String(); got != "客户端自带的" {
		t.Fatalf("内容应转写到标准字段：%s", patched)
	}
	if gjson.GetBytes(patched, "messages.0.reasoning").Exists() {
		t.Fatalf("有毒字段 reasoning 必须删除：%s", patched)
	}
}

// 客户端已带标准方言时原样保留（不查询存档、不重复改写）。
func TestPatchReasoningContentKeepsStandardReasoning(t *testing.T) {
	body := `{"messages":[{"role":"assistant","reasoning_content":"客户端自带的","tool_calls":[{"id":"call_1"}]}]}`
	patched := patchReasoningContent([]byte(body), func([]string) storedReasoning {
		t.Fatal("已有标准思考内容时不应查询存档")
		return storedReasoning{}
	})
	if string(patched) != body {
		t.Fatalf("客户端自带的思考内容必须原样保留：%s", patched)
	}
}

// 客户端重建历史时会写出 "reasoning_content": null（gjson 的 Exists() 判定为 true），
// 必须按「未提供」处理并回填，否则 null 透传给严格上游会直接 400。
func TestPatchReasoningContentFillsNullReasoning(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":"","reasoning_content":null,"tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning {
		return storedReasoning{Field: reasoningFieldStandard, Text: "真实思考"}
	})
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content").String(); got != "真实思考" {
		t.Fatalf("null 字段必须被回填：%s", patched)
	}
}

// 查不到存档时也不能把 null 留给上游：归一成空串（上游接受空串，但拒绝 null）。
func TestPatchReasoningContentNormalizesNullWithoutArchive(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","reasoning_content":null,"tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning { return storedReasoning{} })
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content"); got.Type != gjson.String || got.String() != "" {
		t.Fatalf("无存档时 null 应归一成空串：%s", patched)
	}
}

// 空串同样视为待回填（客户端拿到过空思考时）。
func TestPatchReasoningContentFillsEmptyStringReasoning(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","reasoning_content":"","tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning {
		return storedReasoning{Field: reasoningFieldStandard, Text: "真实思考"}
	})
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content").String(); got != "真实思考" {
		t.Fatalf("空串应被回填：%s", patched)
	}
}

// 存档字段名为空时（异常数据）按标准字段名回传，不写出空键。
func TestPatchReasoningContentFallsBackToStandardField(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning {
		return storedReasoning{Text: "思考"}
	})
	if got := gjson.GetBytes(patched, "messages.0.reasoning_content").String(); got != "思考" {
		t.Fatalf("字段名为空时应回退标准字段：%s", patched)
	}
}

func TestPatchReasoningContentIgnoresMessagesWithoutToolCalls(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":"普通回答"},{"role":"assistant","content":"","tool_calls":[]}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning {
		t.Fatal("没有工具调用时不应查询存档")
		return storedReasoning{}
	})
	if string(patched) != string(body) {
		t.Fatalf("请求体应保持不变：%s", patched)
	}
}

// 当前轮（最后一条 user 消息之后）的工具调用消息即使查不到存档也必须带上字段：
// 上游用「字段缺失」判定未回传而 400，空串则可接受。
func TestPatchReasoningContentAddsEmptyReasoningForCurrentTurnToolCalls(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"跑一下"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning { return storedReasoning{} })
	got := gjson.GetBytes(patched, "messages.1.reasoning_content")
	if got.Type != gjson.String || got.String() != "" {
		t.Fatalf("当前轮工具调用消息必须补空串：%s", patched)
	}
	if gjson.GetBytes(patched, "messages.0.reasoning_content").Exists() {
		t.Fatalf("user 消息不应被改写：%s", patched)
	}
	if gjson.GetBytes(patched, "messages.2.reasoning_content").Exists() {
		t.Fatalf("tool 消息不应被改写：%s", patched)
	}
}

// 当前轮的判定覆盖「最后一条 user 消息之后的所有 assistant 工具调用消息」，
// 不要求它紧邻结尾（实测 [.., tool, assistant("done")] 同样会 400）。
func TestPatchReasoningContentAddsEmptyReasoningBeforeTrailingAssistantText(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"跑一下"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1","content":"ok"},{"role":"assistant","content":"done"}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning { return storedReasoning{} })
	if got := gjson.GetBytes(patched, "messages.1.reasoning_content"); got.Type != gjson.String || got.String() != "" {
		t.Fatalf("当前轮工具调用消息必须补空串：%s", patched)
	}
}

// 已被后续 user 消息「翻篇」的历史消息不参与上游校验，不应平白加字段。
func TestPatchReasoningContentLeavesArchivedToolCallsUntouched(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"跑一下"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1","content":"ok"},{"role":"user","content":"继续"}]}`)
	patched := patchReasoningContent(body, func([]string) storedReasoning { return storedReasoning{} })
	if string(patched) != string(body) {
		t.Fatalf("已翻篇的历史消息不应被改写：%s", patched)
	}
}

func TestPatchReasoningContentIgnoresNonChatBody(t *testing.T) {
	for _, body := range []string{`{"input":"hello"}`, `{"messages":"oops"}`, `not json`} {
		patched := patchReasoningContent([]byte(body), func([]string) storedReasoning {
			t.Fatal("非 Chat 请求体不应查询存档")
			return storedReasoning{}
		})
		if string(patched) != body {
			t.Fatalf("请求体应保持不变：%s", patched)
		}
	}
}

func TestStoredReasoningRoundTrip(t *testing.T) {
	encoded := encodeStoredReasoning(reasoningFieldCompact, "思考内容")
	decoded := decodeStoredReasoning(encoded)
	if decoded.Field != reasoningFieldCompact || decoded.Text != "思考内容" {
		t.Fatalf("存档往返失败：%+v", decoded)
	}
	// 字段名为空时补标准字段名。
	if decoded = decodeStoredReasoning(encodeStoredReasoning("", "思考")); decoded.Field != reasoningFieldStandard {
		t.Fatalf("空字段名应补标准名：%+v", decoded)
	}
	// 兼容早期纯文本存档。
	if decoded = decodeStoredReasoning("早期纯文本"); decoded.Field != reasoningFieldStandard || decoded.Text != "早期纯文本" {
		t.Fatalf("纯文本存档兼容失败：%+v", decoded)
	}
	// 损坏的 JSON 视为无存档。
	if decoded = decodeStoredReasoning(`{"field":`); decoded.Text != "" {
		t.Fatalf("损坏存档应视为空：%+v", decoded)
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
