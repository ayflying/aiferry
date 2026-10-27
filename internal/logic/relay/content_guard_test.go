package relay

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

// 非流式：正文整体是思考块时，思考搬进 reasoning_content，正文只剩回答。
func TestInspectBufferedContentSplitsThinkingBlock(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"<thinking>先想清楚\n</thinking>最终回答"}}]}`)
	fixed, markup := inspectBufferedContent("/chat/completions", body)
	if markup {
		t.Fatal("思考块不应判定为协议标记")
	}
	if got := gjson.GetBytes(fixed, "choices.0.message.reasoning_content").String(); got != "先想清楚\n" {
		t.Fatalf("reasoning_content=%q", got)
	}
	if got := gjson.GetBytes(fixed, "choices.0.message.content").String(); got != "最终回答" {
		t.Fatalf("content=%q", got)
	}
}

// 非流式：没有结束标签时整段正文都是思考，正文置空而不是留一段标签。
func TestInspectBufferedContentThinkingWithoutCloser(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<thinking>只有思考没有回答"}}]}`)
	fixed, markup := inspectBufferedContent("/chat/completions", body)
	if markup {
		t.Fatal("思考块不应判定为协议标记")
	}
	if got := gjson.GetBytes(fixed, "choices.0.message.reasoning_content").String(); got != "只有思考没有回答" {
		t.Fatalf("reasoning_content=%q", got)
	}
	if got := gjson.GetBytes(fixed, "choices.0.message.content").String(); got != "" {
		t.Fatalf("content=%q", got)
	}
}

// 生产实证形态（workbuddy 渠道 afreq_0849474…）：正文是一整坨客户端工具协议标记，
// 必须判定为上游坏响应，绝不能当回答交付。
func TestInspectBufferedContentRejectsToolMarkup(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<tool_result name=read>\n<path>D:\\a.go</path>\n<content>1: package a</content>\n</` + "｜｜DSML｜｜" + ` parameter>"}}]}`)
	if _, markup := inspectBufferedContent("/chat/completions", body); !markup {
		t.Fatal("客户端工具协议标记应判定为不可交付")
	}
}

// 生产实证形态（adesk 渠道 afreq_04b72bca01…）：上游照抄客户端上下文压缩格式，
// 把一段 <summary> 摘要当成本轮回答，同样不能交付。
func TestInspectBufferedContentRejectsSummaryEnvelope(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<summary>\n1. 用户当前两项要求：…\n</summary>"}}]}`)
	if _, markup := inspectBufferedContent("/chat/completions", body); !markup {
		t.Fatal("摘要信封应判定为不可交付")
	}
}

// 客户端上下文压缩本身用 <compacted-summary> 承载，不能被守卫拦掉。
func TestInspectBufferedContentKeepsCompactedSummary(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<compacted-summary>## Primary Request\n- 用户要求…</compacted-summary>"}}]}`)
	fixed, markup := inspectBufferedContent("/chat/completions", body)
	if markup || string(fixed) != string(body) {
		t.Fatalf("压缩结果不应被拦截：markup=%v body=%s", markup, fixed)
	}
}

// 上游已经按标准字段分离思考时不动正文。
func TestInspectBufferedContentKeepsStandardField(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<thinking>这是正文里的标签","reasoning_content":"已有思考"}}]}`)
	if fixed, markup := inspectBufferedContent("/chat/completions", body); markup || string(fixed) != string(body) {
		t.Fatalf("body changed: %s", fixed)
	}
}

// 普通正文整段不动，也不额外解析。
func TestInspectBufferedContentKeepsPlainContent(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"普通回答"}}]}`)
	if fixed, markup := inspectBufferedContent("/chat/completions", body); markup || string(fixed) != string(body) {
		t.Fatalf("body changed: %s", fixed)
	}
}

// 流式：思考块开头分散在多个 chunk 里也要接得起来，正文只留真正的回答。
func TestStreamContentGuardRewritesThinking(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	lines := []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"<thinking>"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"先分析数据"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"</thinking>最终结论"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"，补充一句"}}]}`,
	}
	var reasoning, content string
	for _, line := range lines {
		out, markup := guard.Observe([]byte("data: " + line + "\n"))
		if markup {
			t.Fatalf("思考块不应被拒绝：%s", line)
		}
		if out == nil {
			continue
		}
		payload := out[len("data: "):]
		reasoning += gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String()
		content += gjson.GetBytes(payload, "choices.0.delta.content").String()
	}
	if reasoning != "先分析数据" {
		t.Fatalf("reasoning=%q", reasoning)
	}
	if content != "最终结论，补充一句" {
		t.Fatalf("content=%q", content)
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

// 以 "<" 开头的正常正文不能被误判：判定失败后按原样整段放行。
func TestStreamContentGuardReleasesNonTagContent(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	first := []byte(`data: {"choices":[{"index":0,"delta":{"content":"<t"}}]}` + "\n")
	if out, markup := guard.Observe(first); markup || out != nil {
		t.Fatalf("疑似标签的开头应先缓冲，markup=%v out=%s", markup, out)
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
		t.Fatal("识别失败时不应写入 reasoning_content")
	}
}

// 判定完成后后续正文直通，不再解析。
func TestStreamContentGuardPassesThroughAfterDecision(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	plain := []byte(`data: {"choices":[{"index":0,"delta":{"content":"普通回答"}}]}` + "\n")
	if out, _ := guard.Observe(plain); string(out) != string(plain) {
		t.Fatalf("out=%s", out)
	}
	next := []byte(`data: {"choices":[{"index":0,"delta":{"content":"<thinking>后续不该再处理"}}]}` + "\n")
	if out, _ := guard.Observe(next); string(out) != string(next) {
		t.Fatalf("判定完成后应直通，out=%s", out)
	}
}

// 非 chat 端点（responses/messages）不参与守卫，避免误伤其它协议结构。
func TestContentGuardSkipsOtherEndpoints(t *testing.T) {
	if guard := newStreamContentGuard("/responses"); guard != nil {
		t.Fatal("非 Chat Completions 上游不应启用守卫")
	}
	body := []byte(`{"choices":[{"message":{"content":"<thinking>x</thinking>y"}}]}`)
	if fixed, markup := inspectBufferedContent("/responses", body); markup || string(fixed) != string(body) {
		t.Fatalf("非 chat 端点不应改写：%s", fixed)
	}
}

// 改写后的行必须是合法 JSON 且保留 index/id 等信封字段。
func TestStreamContentGuardKeepsEnvelopeFields(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	out, markup := guard.Observe([]byte(`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"<thinking>x</thinking>y"}}]}` + "\n"))
	if markup || out == nil {
		t.Fatalf("应识别并改写，markup=%v", markup)
	}
	payload := out[len("data: "):]
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("改写结果不是合法 JSON：%v", err)
	}
	if gjson.GetBytes(payload, "id").String() != "c1" || gjson.GetBytes(payload, "choices.0.index").Int() != 0 {
		t.Fatalf("信封字段丢失：%s", payload)
	}
	if got := gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String(); got != "x" {
		t.Fatalf("reasoning_content=%q", got)
	}
	if got := gjson.GetBytes(payload, "choices.0.delta.content").String(); got != "y" {
		t.Fatalf("content=%q", got)
	}
}