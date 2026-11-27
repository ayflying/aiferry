package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestParseLoginInput 覆盖管理员粘回来的各种形态：裸授权码、完整回调地址，
// 以及明显非法或授权被拒的输入。
func TestParseLoginInput(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "bare code", input: "4/1Af-example_code", want: "4/1Af-example_code"},
		{name: "callback url", input: "http://localhost:51121/oauth-callback?code=4/abc&state=s1", want: "4/abc"},
		{name: "denied by google", input: "http://localhost:51121/oauth-callback?error=access_denied", wantErr: true},
		{name: "empty", input: "   ", wantErr: true},
		{name: "url without code", input: "http://localhost:51121/oauth-callback?state=s1", wantErr: true},
		{name: "whitespace inside code", input: "4/abc def", wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseLoginInput(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

// setOAuthClient 注入一对测试用的客户端凭据：真实凭据只从部署环境读取。
func setOAuthClient(t *testing.T) {
	t.Helper()
	t.Setenv(oauthClientIDEnv, "test-client-id")
	t.Setenv(oauthClientSecretEnv, "test-client-secret")
}

// TestLoginSessionsSingleUse 保证一次授权会话只能用一次，且授权地址带齐
// PKCE 与固定回调这些 Google 会校验的参数。
func TestLoginSessionsSingleUse(t *testing.T) {
	setOAuthClient(t)
	sessions := newLoginSessions()
	state, authURL, err := sessions.start("alice")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	for _, fragment := range []string{
		"code_challenge_method=S256",
		"code_challenge=",
		"redirect_uri=",
		"access_type=offline",
		"state=" + state,
	} {
		if !strings.Contains(authURL, fragment) {
			t.Fatalf("authorize URL is missing %q: %s", fragment, authURL)
		}
	}
	verifier, ok := sessions.take(state, "alice")
	if !ok || verifier == "" {
		t.Fatal("expected to exchange the session once")
	}
	if _, ok := sessions.take(state, "alice"); ok {
		t.Fatal("session must not be reusable")
	}
}

// TestLoginSessionsRejectsOtherAccount 保证别的桥接账号兑现不了别人的会话。
func TestLoginSessionsRejectsOtherAccount(t *testing.T) {
	setOAuthClient(t)
	sessions := newLoginSessions()
	state, _, err := sessions.start("alice")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if _, ok := sessions.take(state, "bob"); ok {
		t.Fatal("another account must not resolve the session")
	}
}

// TestLoginSessionsExpired 保证过期会话无法兑现。
func TestLoginSessionsExpired(t *testing.T) {
	setOAuthClient(t)
	sessions := newLoginSessions()
	state, _, err := sessions.start("alice")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	sessions.items[state] = loginSession{account: "alice", verifier: "verifier", expires: time.Now().Add(-time.Second)}
	if _, ok := sessions.take(state, "alice"); ok {
		t.Fatal("expired session must not be exchangeable")
	}
}

// TestLoginRequiresOAuthClient 确认未配置客户端凭据时，发起登录会明确报错，
// 而不是生成一个注定被 Google 拒绝的授权地址。
func TestLoginRequiresOAuthClient(t *testing.T) {
	t.Setenv(oauthClientIDEnv, "")
	t.Setenv(oauthClientSecretEnv, "")
	if _, _, err := newLoginSessions().start("alice"); err == nil {
		t.Fatal("expected an error when the OAuth client credentials are missing")
	}
}

// TestAntigravityScopesExcludeOpenID 是一条防御性断言：把 openid 加进 scope
// 会让 Google 走到会卡住的同意流程，这个坑不能被后续改动重新引入。
func TestAntigravityScopesExcludeOpenID(t *testing.T) {
	for _, scope := range antigravityScopes {
		if scope == "openid" {
			t.Fatal("openid must not be requested")
		}
	}
}

// TestWriteAntigravityToken 校验令牌文件落在官方 CLI 读取的路径上，
// 权限是 0600，结构是 {"token":{...},"auth_method":"consumer"}。
func TestWriteAntigravityToken(t *testing.T) {
	home := t.TempDir()
	document := antigravityTokenDoc{
		Token:      antigravityToken{AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh", Expiry: "2026-01-01T00:00:00.000Z"},
		AuthMethod: "consumer",
	}
	if err := writeAntigravityToken(home, document); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	target := filepath.Join(home, antigravityTokenPath)
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("token file is missing: %v", err)
	}
	// Windows 不实现 Unix 权限位，os.WriteFile 的 0600 在那里退化成 0666；
	// 生产运行在 Linux 容器里，权限断言只在非 Windows 上生效。
	if runtime.GOOS != "windows" {
		if permission := info.Mode().Perm(); permission != 0600 {
			t.Fatalf("token file permission is %o, want 600", permission)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var decoded antigravityTokenDoc
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("token file is not valid JSON: %v", err)
	}
	if decoded.Token.RefreshToken != "refresh" || decoded.AuthMethod != "consumer" || decoded.Token.AccessToken != "access" {
		t.Fatalf("unexpected token file content: %s", string(data))
	}
}
