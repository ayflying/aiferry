package relay

import (
	"testing"

	"github.com/tidwall/gjson"
)

// 生产实证形态（workbuddy 渠道 afreq_0849474…）：正文是一整坨客户端工具协议标记，
// 必须判定为上游坏响应，绝不能当回答交付。
func TestInspectBufferedMarkupRejectsToolMarkup(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<tool_result name=read>\n<path>D:\\a.go</path>\n<content>1: package a</content>\n</` + "｜｜DSML｜｜" + ` parameter>"}}]}`)
	if !inspectBufferedMarkup("/chat/completions", body) {
		t.Fatal("客户端工具协议标记应判定为不可交付")
	}
}

// 生产实证形态（adesk 渠道 afreq_04b72bca01…）：上游照抄客户端上下文压缩格式，
// 把一段 <summary> 摘要当成本轮回答，同样不能交付。
func TestInspectBufferedMarkupRejectsSummaryEnvelope(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<summary>\n1. 用户当前两项要求：…\n</summary>"}}]}`)
	if !inspectBufferedMarkup("/chat/completions", body) {
		t.Fatal("摘要信封应判定为不可交付")
	}
}

// 客户端上下文压缩本身用 <compacted-summary> 承载，不能被守卫拦掉。
func TestInspectBufferedMarkupKeepsCompactedSummary(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<compacted-summary>## Primary Request\n- 用户要求…</compacted-summary>"}}]}`)
	if inspectBufferedMarkup("/chat/completions", body) {
		t.Fatal("压缩结果不应被拦截")
	}
}

// 普通正文放行。
func TestInspectBufferedMarkupKeepsPlainContent(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"普通回答"}}]}`)
	if inspectBufferedMarkup("/chat/completions", body) {
		t.Fatal("普通正文不应被拦截")
	}
}

// 2026-10-29 起思考标签不再特判：<thinking>/<analysis> 写进正文时按普通正文交付，
// 网关不搬移、不改写（上游已修复标签错乱，模型专用补丁已删除）。
func TestInspectBufferedMarkupPassesThinkingTags(t *testing.T) {
	for _, content := range []string{
		`<thinking>先想清楚\n</thinking>最终回答`,
		`<analysis>梳理一下</analysis>`,
	} {
		body := []byte(`{"choices":[{"message":{"role":"assistant","content":"` + content + `"}}]}`)
		if inspectBufferedMarkup("/chat/completions", body) {
			t.Fatalf("思考标签不应被判成协议标记：%s", content)
		}
	}
}

// 非 chat 端点（responses/messages）不参与判定，避免误伤其它协议结构。
func TestInspectBufferedMarkupSkipsOtherEndpoints(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<tool_result name=read>"}}]}`)
	if inspectBufferedMarkup("/responses", body) {
		t.Fatal("非 chat 端点不应判定")
	}
}

// 流式：正文开头就是客户端工具协议标记时拒绝该响应（不得写出任何字节）。
func TestStreamContentGuardRejectsToolMarkup(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	if out, markup := guard.Observe([]byte(`data: {"choices":[{"index":0,"delta":{"content":"<tool_result name=read>"}}]}` + "\n")); !markup {
		t.Fatalf("应拒绝协议标记，out=%s", out)
	}
}

// 流式：摘要信封同样拒绝。
func TestStreamContentGuardRejectsSummaryEnvelope(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	if out, markup := guard.Observe([]byte(`data: {"choices":[{"index":0,"delta":{"content":"<summary>\n1. 用户要求"}}]}` + "\n")); !markup {
		t.Fatalf("应拒绝摘要信封，out=%s", out)
	}
}

// 流式：标记被切成两半下发时，前面的半截必须先缓冲，等后面的字节到齐再判。
func TestStreamContentGuardRejectsSplitMarkup(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	first := []byte(`data: {"choices":[{"index":0,"delta":{"content":"<tool_res"}}]}` + "\n")
	if out, markup := guard.Observe(first); markup || out != nil {
		t.Fatalf("半截标记应先缓冲，markup=%v out=%s", markup, out)
	}
	second := []byte(`data: {"choices":[{"index":0,"delta":{"content":"ult name=read>"}}]}` + "\n")
	if out, markup := guard.Observe(second); !markup {
		t.Fatalf("拼齐后应拒绝，out=%s", out)
	}
}

// 流式：思考标签逐片原样返回，不再搬进 reasoning_content（模型专用补丁已删除）。
func TestStreamContentGuardPassesThinkingTags(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	lines := []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"<thinking>"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"先分析数据"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"</thinking>最终结论"}}]}`,
	}
	for _, line := range lines {
		raw := "data: " + line + "\n"
		out, markup := guard.Observe([]byte(raw))
		if markup {
			t.Fatalf("思考标签不应被拒绝：%s", line)
		}
		if string(out) != raw {
			t.Fatalf("思考标签应原样透传：want=%s got=%s", raw, out)
		}
	}
}

// 以 "<" 开头的正常正文不能被误判：判定失败后按原样整段放行。
func TestStreamContentGuardReleasesNonTagContent(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	first := []byte(`data: {"choices":[{"index":0,"delta":{"content":"<t"}}]}` + "\n")
	if out, markup := guard.Observe(first); markup || out != nil {
		t.Fatalf("疑似标记的开头应先缓冲，markup=%v out=%s", markup, out)
	}
	second := []byte(`data: {"choices":[{"index":0,"delta":{"content":"able> 标签示例"}}]}` + "\n")
	out, markup := guard.Observe(second)
	if markup {
		t.Fatal("普通正文不应被拒绝")
	}
	if out == nil {
		t.Fatal("识别失败时应把缓冲的正文整段放行")
	}
	payload := out[len("data: "):]
	if got := gjson.GetBytes(payload, "choices.0.delta.content").String(); got != "<table> 标签示例" {
		t.Fatalf("content=%q", got)
	}
	if gjson.GetBytes(payload, "choices.0.delta.reasoning_content").Exists() {
		t.Fatal("放行时不应写入 reasoning_content")
	}
}

// 判定完成后后续正文直通，不再解析。
func TestStreamContentGuardPassesThroughAfterDecision(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	plain := []byte(`data: {"choices":[{"index":0,"delta":{"content":"普通回答"}}]}` + "\n")
	if out, _ := guard.Observe(plain); string(out) != string(plain) {
		t.Fatalf("out=%s", out)
	}
	next := []byte(`data: {"choices":[{"index":0,"delta":{"content":"<tool_result name=read>"}}]}` + "\n")
	if out, _ := guard.Observe(next); string(out) != string(next) {
		t.Fatalf("判定完成后应直通，out=%s", out)
	}
}

// 判定窗口里的字节遇到工具调用时要并到同一行交付，不能丢正文。
func TestStreamContentGuardMergesBufferedOnToolCall(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	first := []byte(`data: {"choices":[{"index":0,"delta":{"content":"<t"}}]}` + "\n")
	if out, _ := guard.Observe(first); out != nil {
		t.Fatalf("开头字节应缓冲，out=%s", out)
	}
	second := []byte(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function"}]}}]}` + "\n")
	out, markup := guard.Observe(second)
	if markup || out == nil {
		t.Fatalf("工具调用应合并缓冲正文交付，markup=%v out=%s", markup, out)
	}
	payload := out[len("data: "):]
	if got := gjson.GetBytes(payload, "choices.0.delta.content").String(); got != "<t" {
		t.Fatalf("缓冲正文丢失：content=%q", got)
	}
	if got := gjson.GetBytes(payload, "choices.0.delta.tool_calls.0.id").String(); got != "call_1" {
		t.Fatalf("工具调用字段丢失：id=%q", got)
	}
}

// 非 chat 端点（responses/messages）不参与守卫，避免误伤其它协议结构。
func TestContentGuardSkipsOtherEndpoints(t *testing.T) {
	if guard := newStreamContentGuard("/responses"); guard != nil {
		t.Fatal("非 Chat Completions 上游不应启用守卫")
	}
	body := []byte(`{"choices":[{"message":{"content":"<tool_result name=read>"}}]}`)
	if inspectBufferedMarkup("/responses", body) {
		t.Fatal("非 chat 端点不应判定")
	}
}
