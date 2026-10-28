package relay

import (
	"testing"

	"github.com/tidwall/gjson"
)

// 生产实证形态（adesk 渠道 afreq_fba9d1d66070e5bf78235cea，2026-10-28）：
// 模型跑偏时先把「按时间顺序梳理」的草稿写进 <analysis>，紧接着又输出一段
// <summary> 摘要信封，整段都被当成本轮回答。摘要部分绝不能交付。
func TestInspectBufferedContentRejectsAnalysisThenSummary(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<analysis>\n让我按时间顺序梳理这段对话。\n</analysis>\n<summary>\n1. 更新 docs/release-connection-robots.md\n</summary>"}}]}`)
	if _, markup := inspectBufferedContent("/chat/completions", body); !markup {
		t.Fatal("analysis 之后的摘要信封应判定为不可交付")
	}
}

// <analysis> 之后接着真正的回答时，草稿搬进 reasoning_content，回答照常交付。
func TestInspectBufferedContentSplitsAnalysisThenAnswer(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"<analysis>先看日志再下手</analysis>已经改好了，请复查。"}}]}`)
	fixed, markup := inspectBufferedContent("/chat/completions", body)
	if markup {
		t.Fatal("analysis 草稿不是协议标记")
	}
	if got := gjson.GetBytes(fixed, "choices.0.message.reasoning_content").String(); got != "先看日志再下手" {
		t.Fatalf("reasoning_content=%q", got)
	}
	if got := gjson.GetBytes(fixed, "choices.0.message.content").String(); got != "已经改好了，请复查。" {
		t.Fatalf("content=%q", got)
	}
}

// 流式同形态：<analysis> 与 <summary> 落在同一个分片里，必须拒绝。
func TestStreamContentGuardRejectsAnalysisSummaryChunk(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	line := `data: {"choices":[{"index":0,"delta":{"content":"<analysis>梳理一下</analysis><summary>1. 干完了</summary>"}}]}` + "\n"
	if _, markup := guard.Observe([]byte(line)); !markup {
		t.Fatal("整段信封应判定为不可交付")
	}
}

// 流式：<analysis> 与 <summary> 分在两个分片里，第二个分片仍要拦住。
func TestStreamContentGuardRejectsSummaryInNextChunk(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	first := `data: {"choices":[{"index":0,"delta":{"content":"<analysis>梳理一下</analysis>"}}]}` + "\n"
	if _, markup := guard.Observe([]byte(first)); markup {
		t.Fatal("草稿本身不应被拒绝")
	}
	second := `data: {"choices":[{"index":0,"delta":{"content":"<summary>1. 干完了</summary>"}}]}` + "\n"
	if _, markup := guard.Observe([]byte(second)); !markup {
		t.Fatal("下一分片里的摘要信封应判定为不可交付")
	}
}

// 流式：<analysis> 之后是真正的回答，思考搬进 reasoning_content，正文照常交付。
func TestStreamContentGuardSplitsAnalysisThenAnswer(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	line := `data: {"id":"c2","choices":[{"index":0,"delta":{"content":"<analysis>先看日志</analysis>已经改好了"}}]}` + "\n"
	out, markup := guard.Observe([]byte(line))
	if markup || out == nil {
		t.Fatalf("应识别并改写，markup=%v", markup)
	}
	payload := out[len("data: "):]
	if got := gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String(); got != "先看日志" {
		t.Fatalf("reasoning_content=%q", got)
	}
	if got := gjson.GetBytes(payload, "choices.0.delta.content").String(); got != "已经改好了" {
		t.Fatalf("content=%q", got)
	}
}

// <analysis> 没给结束标签时，整段正文都算草稿，正文置空。
func TestStreamContentGuardAnalysisWithoutCloser(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	line := `data: {"choices":[{"index":0,"delta":{"content":"<analysis>只写了草稿"}}]}` + "\n"
	out, markup := guard.Observe([]byte(line))
	if markup || out == nil {
		t.Fatalf("草稿不应被拒绝，markup=%v", markup)
	}
	payload := out[len("data: "):]
	if got := gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String(); got != "只写了草稿" {
		t.Fatalf("reasoning_content=%q", got)
	}
	if got := gjson.GetBytes(payload, "choices.0.delta.content").String(); got != "" {
		t.Fatalf("content=%q", got)
	}
}
