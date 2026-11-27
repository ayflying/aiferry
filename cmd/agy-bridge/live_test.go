package main

import (
	"crypto/sha256"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 本文件的真实测试由已登录官方 CLI 的操作者显式开启（AGY_BRIDGE_LIVE_TEST=1），
// 会消耗个人额度或访问 Google，不在 CI 运行。

// liveCLI 返回本机已登录的官方 CLI 路径，找不到即跳过测试。
func liveCLI(t *testing.T) string {
	t.Helper()
	if os.Getenv("AGY_BRIDGE_LIVE_TEST") != "1" {
		t.Skip("requires local official agy authorization")
	}
	path, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("official agy cli not installed locally")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// liveAccount 沿用操作者自己的 HOME（官方 CLI 登录态所在处）构造默认账号。
func liveAccount(t *testing.T) account {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return account{name: "default", token: sha256.Sum256([]byte("Bearer test")), home: home}
}

func TestLiveOfficialCLI(t *testing.T) {
	path := liveCLI(t)
	policy := filepath.Join(t.TempDir(), "guard.json")
	if err := os.WriteFile(policy, []byte(guard), 0600); err != nil {
		t.Fatal(err)
	}
	const model = "gemini-4-hydrogen-low"
	h := newBridge([]account{liveAccount(t)}, model, accountRunner(path, t.TempDir(), policy, model), accountUsage(path, t.TempDir()), testCLI(t))
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","stream":true,"max_completion_tokens":16,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"Reply with exactly OK. Do not use tools."}]}`))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"content":"OK`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("official CLI did not complete a valid OK stream: HTTP %d", w.Code)
	}
}

// TestLiveUsage 验证官方 CLI 的 /usage 能通过桥接的额度端点拿到结构化的
// 5 小时与每周窗口（2026-11-27 实测约 8 秒）。
func TestLiveUsage(t *testing.T) {
	path := liveCLI(t)
	h := newBridge([]account{liveAccount(t)}, "unused", nil, accountUsage(path, t.TempDir()), testCLI(t))
	r := httptest.NewRequest("GET", "/v1/quota", nil)
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rolling"`) || !strings.Contains(w.Body.String(), `"weekly"`) {
		t.Fatalf("official CLI /usage did not produce both windows: HTTP %d %s", w.Code, w.Body.String())
	}
}

// TestLiveCLIUpdate 真实走一遍更新器：读官方清单、下载、sha512 校验、落盘。
// 这是「镜像不带 agy、启动时自取最新」的端到端证明；需要本机能访问 Google。
// 下载的是 linux/amd64 构建：类 Unix 系统上再执行一次确认可用，Windows 本机
// 只能核对落盘文件与官方发布包一致。
func TestLiveCLIUpdate(t *testing.T) {
	liveCLI(t)
	m := newCLIManager(filepath.Join(t.TempDir(), "cli"), globalProxy)
	if err := m.ensure(t.Context()); err != nil {
		t.Fatalf("cli updater failed: %v", err)
	}
	version := m.currentVersion()
	if version == "" {
		t.Fatal("cli updater did not record a version")
	}
	info, err := os.Stat(m.binary())
	if err != nil || info.Size() < 1<<20 {
		t.Fatalf("installed cli missing or implausibly small: %v %v", err, info)
	}
	if runtime.GOOS != "windows" {
		out, err := exec.CommandContext(t.Context(), m.binary(), "--version").CombinedOutput()
		if err != nil || !strings.Contains(string(out), version) {
			t.Fatalf("installed cli does not report version %s: %v %s", version, err, out)
		}
	}
	// 第二次 ensure 必须是幂等的：版本一致就不重新下载。
	if err = m.ensure(t.Context()); err != nil {
		t.Fatalf("second ensure failed: %v", err)
	}
	t.Logf("official cli %s downloaded, verified and installed (%d bytes)", version, info.Size())
}
