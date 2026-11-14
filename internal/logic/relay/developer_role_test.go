package relay

import (
	"encoding/json"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// decodeMessages 把 prepareRequestBody 的产物解出 messages，供断言角色与内容。
func decodeMessages(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	raw, ok := payload["messages"].([]any)
	if !ok {
		t.Fatalf("请求体缺少 messages: %s", body)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		message, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("messages 元素不是对象: %v", value)
		}
		out = append(out, message)
	}
	return out
}

// developer 必须被改写成 system，且指令内容一个字都不能变。
func TestPrepareRequestBodyRewritesDeveloperToSystem(t *testing.T) {
	body := []byte(`{"model":"glm-5.3-flash","messages":[{"role":"developer","content":"必须遵守的规则"},{"role":"user","content":"你好"}]}`)
	prepared, err := prepareRequestBody("/chat/completions", body, "glm-5.3-flash", channel.AdvancedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, prepared)
	if messages[0]["role"] != "system" {
		t.Fatalf("developer 应改写成 system，实际 %v", messages[0]["role"])
	}
	if messages[0]["content"] != "必须遵守的规则" {
		t.Fatalf("指令内容不得改动，实际 %v", messages[0]["content"])
	}
	if messages[1]["role"] != "user" {
		t.Fatalf("其它角色不得改动，实际 %v", messages[1]["role"])
	}
}

// 原始 system 消息保持原样，改写只针对 developer。
func TestPrepareRequestBodyKeepsSystemUntouched(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"原有系统提示"},{"role":"developer","content":"追加规则"},{"role":"user","content":"hi"}]}`)
	prepared, err := prepareRequestBody("/chat/completions", body, "m", channel.AdvancedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, prepared)
	if messages[0]["role"] != "system" || messages[0]["content"] != "原有系统提示" {
		t.Fatalf("原有 system 消息不得改动，实际 %v", messages[0])
	}
	if messages[1]["role"] != "system" || messages[1]["content"] != "追加规则" {
		t.Fatalf("developer 应改写成 system 并保留内容，实际 %v", messages[1])
	}
}

// 多模态 content（parts 数组）与工具字段必须原样保留，只换角色名。
func TestPrepareRequestBodyKeepsDeveloperMultimodalContent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"developer","content":[{"type":"text","text":"看图"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]},{"role":"user","content":"hi"}]}`)
	prepared, err := prepareRequestBody("/chat/completions", body, "m", channel.AdvancedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, prepared)
	if messages[0]["role"] != "system" {
		t.Fatalf("角色应为 system，实际 %v", messages[0]["role"])
	}
	parts, ok := messages[0]["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("多模态 content 必须原样保留，实际 %v", messages[0]["content"])
	}
	first, _ := parts[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "看图" {
		t.Fatalf("文本 part 被改动: %v", first)
	}
}

// 只有一个 developer、没有 system 时，改写后仍是一条合法指令消息，不额外插入。
func TestPrepareRequestBodyDeveloperBecomesSoleSystem(t *testing.T) {
	body := []byte(`{"messages":[{"role":"developer","content":"规则"},{"role":"user","content":"hi"}]}`)
	prepared, err := prepareRequestBody("/chat/completions", body, "m", channel.AdvancedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, prepared)
	if len(messages) != 2 {
		t.Fatalf("不得增删消息，实际 %d 条", len(messages))
	}
	if messages[0]["role"] != "system" {
		t.Fatalf("首条应为 system，实际 %v", messages[0]["role"])
	}
}

// Responses 端点的 input 不在本层改写范围：它的 developer 语义由协议转换处理。
func TestPrepareRequestBodyLeavesResponsesInputAlone(t *testing.T) {
	body := []byte(`{"model":"m","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"规则"}]}]}`)
	prepared, err := prepareRequestBody("/responses", body, "m", channel.AdvancedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(prepared, &payload); err != nil {
		t.Fatal(err)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input 结构被破坏: %s", prepared)
	}
	item, _ := input[0].(map[string]any)
	if item["role"] != "developer" {
		t.Fatalf("Responses 端点的 developer 不应在本层改写，实际 %v", item["role"])
	}
}

// 没有 developer 时请求体内容不变（除 model 之外不引入额外改动）。
func TestPrepareRequestBodyWithoutDeveloperIsUnchanged(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"system","content":"规则"},{"role":"user","content":"hi"}]}`)
	prepared, err := prepareRequestBody("/chat/completions", body, "m", channel.AdvancedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeMessages(t, prepared)
	if len(messages) != 2 || messages[0]["role"] != "system" || messages[1]["role"] != "user" {
		t.Fatalf("无 developer 时不应改动消息，实际 %v", messages)
	}
}

// 纯函数单测：非 chat 端点与缺失 messages 一律返回 0，不改任何内容。
func TestNormalizeDeveloperRoleSkipsUnsupportedShapes(t *testing.T) {
	if got := normalizeDeveloperRole("/responses", map[string]any{"messages": []any{}}); got != 0 {
		t.Fatalf("非 chat 端点不应改写，实际 %d", got)
	}
	if got := normalizeDeveloperRole("/chat/completions", map[string]any{}); got != 0 {
		t.Fatalf("缺少 messages 不应改写，实际 %d", got)
	}
	payload := map[string]any{"messages": []any{"not-an-object", map[string]any{"role": 42}}}
	if got := normalizeDeveloperRole("/chat/completions", payload); got != 0 {
		t.Fatalf("非法元素应跳过，实际 %d", got)
	}
}

// 改写次数要能反映真实条数，供调用方判断是否需要重新编码。
func TestNormalizeDeveloperRoleCountsEveryRewrite(t *testing.T) {
	payload := map[string]any{"messages": []any{
		map[string]any{"role": "developer", "content": "a"},
		map[string]any{"role": "user", "content": "b"},
		map[string]any{"role": "developer", "content": "c"},
	}}
	if got := normalizeDeveloperRole("/chat/completions", payload); got != 2 {
		t.Fatalf("应改写 2 条，实际 %d", got)
	}
}
