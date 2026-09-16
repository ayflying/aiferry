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
