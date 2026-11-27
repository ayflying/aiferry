package main

import (
	"crypto/sha256"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 本测试由已登录官方 CLI 的操作者显式开启，不在 CI 消耗个人额度。
func TestLiveOfficialCLI(t *testing.T) {
	if os.Getenv("AGY_BRIDGE_LIVE_TEST") != "1" {
		t.Skip("requires local official agy authorization")
	}
	path, err := exec.LookPath("agy")
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(t.TempDir(), "guard.json")
	if err := os.WriteFile(policy, []byte(guard), 0600); err != nil {
		t.Fatal(err)
	}
	const model = "gemini-4-hydrogen-low"
	// 本机 live 测试沿用操作者自己的 HOME（官方 CLI 登录态所在处）。
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	acc := account{name: "default", token: sha256.Sum256([]byte("Bearer test")), home: home}
	h := newBridge([]account{acc}, model, accountRunner(path, t.TempDir(), policy, model), accountUsage(path, t.TempDir()))
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","stream":true,"max_completion_tokens":16,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"Reply with exactly OK. Do not use tools."}]}`))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"content":"OK`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("official CLI did not complete a valid OK stream: HTTP %d", w.Code)
	}
}

// TestLiveUsage 同样只由操作者显式开启：验证官方 CLI 的 /usage 能通过桥接的
// 额度端点拿到结构化的 5 小时与每周窗口（2026-11-27 实测约 8 秒）。
func TestLiveUsage(t *testing.T) {
	if os.Getenv("AGY_BRIDGE_LIVE_TEST") != "1" {
		t.Skip("requires local official agy authorization")
	}
	path, err := exec.LookPath("agy")
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	acc := account{name: "default", token: sha256.Sum256([]byte("Bearer test")), home: home}
	h := newBridge([]account{acc}, "unused", nil, accountUsage(path, t.TempDir()))
	r := httptest.NewRequest("GET", "/v1/quota", nil)
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rolling"`) || !strings.Contains(w.Body.String(), `"weekly"`) {
		t.Fatalf("official CLI /usage did not produce both windows: HTTP %d %s", w.Code, w.Body.String())
	}
}
