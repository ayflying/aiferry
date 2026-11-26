package main

import (
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
	h := newBridge("test", model, cliRunner(path, t.TempDir(), policy, model))
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","stream":true,"max_completion_tokens":16,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"Reply with exactly OK. Do not use tools."}]}`))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"content":"OK`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("official CLI did not complete a valid OK stream: HTTP %d", w.Code)
	}
}
