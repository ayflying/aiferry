package relay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPayloadStreamCaptureAggregatesChatDeltas(t *testing.T) {
	capture := newPayloadStreamCapture("/chat/completions")
	capture.observe([]byte(`data: {"model":"m","choices":[{"delta":{"content":"正文A","reasoning_content":"思考A"}}]}` + "\n"))
	capture.observe([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"<analysis>x</analysis>正文B\"}}]}\n"))
	capture.observe([]byte(`data: {"choices":[{"delta":{"reasoning_content":"思考B"}}]}` + "\n"))
	capture.observe([]byte("data: [DONE]\n"))
	content, reasoning := capture.result()
	if content != "正文A<analysis>x</analysis>正文B" {
		t.Fatalf("content = %q", content)
	}
	if reasoning != "思考A思考B" {
		t.Fatalf("reasoning = %q", reasoning)
	}
}

func TestPayloadStreamCaptureAggregatesResponsesDeltas(t *testing.T) {
	capture := newPayloadStreamCapture("/responses")
	capture.observe([]byte(`data: {"type":"response.output_text.delta","delta":"answer"}` + "\n"))
	capture.observe([]byte(`data: {"type":"response.reasoning_text.delta","delta":"thinking"}` + "\n"))
	capture.observe([]byte(`data: {"type":"response.completed"}` + "\n"))
	content, reasoning := capture.result()
	if content != "answer" {
		t.Fatalf("content = %q", content)
	}
	if reasoning != "thinking" {
		t.Fatalf("reasoning = %q", reasoning)
	}
}

func TestPayloadStreamCaptureSkipsInvalidLines(t *testing.T) {
	capture := newPayloadStreamCapture("/chat/completions")
	capture.observe([]byte("event: ping\n"))
	capture.observe([]byte("data: not-json\n"))
	capture.observe([]byte(": keep-alive\n"))
	if content, reasoning := capture.result(); content != "" || reasoning != "" {
		t.Fatalf("expected empty, got %q / %q", content, reasoning)
	}
}

func TestTruncatePayloadField(t *testing.T) {
	data := []byte(strings.Repeat("a", maxPayloadFieldBytes+1))
	truncated, flag := truncatePayloadField(data)
	if !flag || len(truncated) != maxPayloadFieldBytes {
		t.Fatalf("expected truncation to %d with flag, got len=%d flag=%v", maxPayloadFieldBytes, len(truncated), flag)
	}
	small := []byte(`{"ok":true}`)
	out, flag := truncatePayloadField(small)
	if flag || string(out) != string(small) {
		t.Fatalf("expected passthrough, got %q flag=%v", out, flag)
	}
	if raw, flag := truncatePayloadField(nil); raw != nil || flag {
		t.Fatalf("expected nil for empty input, got %v flag=%v", raw, flag)
	}
}

func TestCleanupPayloadsKeepsNewestFiles(t *testing.T) {
	dir := t.TempDir()
	payloadLogState.dir = dir
	payloadLogState.maxFiles = 3
	defer func() {
		payloadLogState.dir = "/app/data/relay-payloads"
		payloadLogState.maxFiles = 9999
	}()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "afreq_"+string(rune('a'+i))+".json")
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, base.Add(time.Duration(i)*time.Minute), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	// 非报文文件不应参与统计与删除
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupPayloads(context.Background())
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	remaining := map[string]bool{}
	for _, e := range entries {
		remaining[e.Name()] = true
	}
	if len(remaining) != 4 {
		t.Fatalf("expected 3 json + 1 txt, got %v", remaining)
	}
	for _, keep := range []string{"afreq_c.json", "afreq_d.json", "afreq_e.json", "ignore.txt"} {
		if !remaining[keep] {
			t.Fatalf("expected %s to survive, got %v", keep, remaining)
		}
	}
	if remaining["afreq_a.json"] || remaining["afreq_b.json"] {
		t.Fatalf("oldest files should be removed, got %v", remaining)
	}
}

// 体积限额：文件数没超，但总体积超了也必须从最旧的开始删。
// 这是 2026-10-29 磁盘写满事故的回归用例——当时只有文件数限额（9999），
// 单条报文最大 1.2MB，目录涨到 3.4GB 撑爆宿主机。
func TestCleanupPayloadsEnforcesByteBudget(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	dir := t.TempDir()
	payloadLogState.dir = dir
	payloadLogState.maxFiles = 100
	payloadLogState.maxBytes = 30
	base := time.Now().Add(-time.Hour)
	// 5 个文件 × 10 字节 = 50 字节，超出 30 字节上限；文件数远未到 100。
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "afreq_"+string(rune('a'+i))+".json")
		if err := os.WriteFile(name, []byte("0123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	cleanupPayloads(context.Background())
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	remaining := map[string]bool{}
	var total int64
	for _, e := range entries {
		remaining[e.Name()] = true
		if info, infoErr := e.Info(); infoErr == nil {
			total += info.Size()
		}
	}
	if total > payloadLogState.maxBytes {
		t.Fatalf("total bytes %d still over budget %d, kept %v", total, payloadLogState.maxBytes, remaining)
	}
	if len(remaining) != 3 {
		t.Fatalf("expected newest 3 files to survive, got %v", remaining)
	}
	for _, keep := range []string{"afreq_c.json", "afreq_d.json", "afreq_e.json"} {
		if !remaining[keep] {
			t.Fatalf("expected %s to survive, got %v", keep, remaining)
		}
	}
}

// 单个报文就超过体积预算时，仍然保留最新这一条：排障最需要的是刚出问题的原文，
// 全删光等于把刚发生的故障现场一起丢掉。
func TestCleanupPayloadsKeepsNewestEvenWhenSingleFileOverBudget(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	dir := t.TempDir()
	payloadLogState.dir = dir
	payloadLogState.maxFiles = 100
	payloadLogState.maxBytes = 5
	name := filepath.Join(dir, "afreq_only.json")
	if err := os.WriteFile(name, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupPayloads(context.Background())
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("newest payload must survive, stat err=%v", err)
	}
}

func TestReadPayloadRejectsUnsafeRequestID(t *testing.T) {
	for _, bad := range []string{"", "../secret", "a/b", "..", "afreq_..\\x", strings.Repeat("x", 200)} {
		if _, err := ReadPayload(bad); err == nil {
			t.Fatalf("expected rejection for %q", bad)
		}
	}
	dir := t.TempDir()
	payloadLogState.dir = dir
	defer func() { payloadLogState.dir = "/app/data/relay-payloads" }()
	if err := os.WriteFile(filepath.Join(dir, "afreq_abc123.json"), []byte(`{"requestId":"afreq_abc123"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadPayload("afreq_abc123")
	if err != nil || !strings.Contains(string(data), "afreq_abc123") {
		t.Fatalf("expected payload read, err=%v data=%q", err, data)
	}
}
