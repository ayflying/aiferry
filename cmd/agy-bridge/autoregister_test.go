package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoRegisterOnLoginStart 覆盖「新渠道第一次登录」这条路径：渠道下还没有任何
// 桥接令牌，AiFerry 自己生成一把再调 /v1/login/start，桥接应当就地登记账号并
// 继续走授权流程，用户不需要先去服务器上找令牌再手工粘贴。
func TestAutoRegisterOnLoginStart(t *testing.T) {
	setOAuthClient(t)
	state := t.TempDir()
	token := strings.Repeat("a", 64)
	h := newBridge(nil, "example", nil, nil, testCLI(t), state, true)

	req := httptest.NewRequest("POST", "/v1/login/start", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(state, accountDirName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("want exactly one account dir, got %d (err=%v)", len(entries), err)
	}
	// 落盘的令牌必须能被后续请求原样识别，否则第一次登录成功后仍然调不通。
	if _, ok := resolve(mustLoad(t, state), "Bearer "+token); !ok {
		t.Fatal("registered token is not resolvable after restart")
	}
}

// TestAutoRegisterOnlyOnLoginStart 保证自动登记只对登录入口开放：其余端点拿未知
// 令牌必须仍然 401，否则任何人随手编一个字符串就能绕过认证去调模型。
func TestAutoRegisterOnlyOnLoginStart(t *testing.T) {
	state := t.TempDir()
	h := newBridge(nil, "example", nil, nil, testCLI(t), state, true)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 64))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(state, accountDirName)); err == nil {
		t.Fatal("models endpoint must not create accounts")
	}
}

// TestAutoRegisterDisabled 确认开关关闭时行为与改造前一致，老部署不受影响。
func TestAutoRegisterDisabled(t *testing.T) {
	state := t.TempDir()
	h := newBridge(nil, "example", nil, nil, testCLI(t), state, false)

	req := httptest.NewRequest("POST", "/v1/login/start", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("c", 64))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestCreateAccountDirRejectsWeakToken 拦住过短令牌，避免弱口令进入认证环节。
func TestCreateAccountDirRejectsWeakToken(t *testing.T) {
	if _, err := createAccountDir(t.TempDir(), "Bearer short"); err == nil {
		t.Fatal("want error for weak token")
	}
}

// mustLoad 从卷里重新读账号，模拟桥接重启后重新解析令牌的过程。
func mustLoad(t *testing.T, stateDir string) []account {
	t.Helper()
	accounts, err := loadAccounts(stateDir, false)
	if err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	return accounts
}
