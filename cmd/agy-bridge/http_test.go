package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// testAccount 构造一个仅用于测试的账号（HOME 指向临时目录，不调用真实 CLI）。
// 令牌满足生产要求的最低强度，避免测试与真实校验规则脱节。
const testToken = "test-token-0123456789abcdef0123456789abcdef"

// testCLI 构造一个只读的 CLI 管理器：单测不触网、不装 CLI，只让 /v1/version 有值可读。
func testCLI(t *testing.T) *cliManager {
	t.Helper()
	m := newCLIManager(t.TempDir(), func() string { return "" })
	if err := os.WriteFile(m.record(), []byte(`{"version":"test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return m
}

func testAccount(t *testing.T, name string) account {
	t.Helper()
	acc, err := newAccount(name, testToken, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func TestBridge(t *testing.T) {
	acc := testAccount(t, "alice")
	run := func(_ account, _ string) runner {
		return func(_ context.Context, p string, emit func(string) error) (usage, error) {
			if p != "ping" {
				t.Fatalf("unexpected prompt %q", p)
			}
			return usage{Input: 2, Output: 1, Total: 3}, emit("OK")
		}
	}
	usageFor := func(_ account, _ string) func(context.Context) (usagePayload, error) {
		return func(context.Context) (usagePayload, error) {
			rolling := usageWindow{Status: "ok"}
			weekly := usageWindow{Status: "ok"}
			var payload usagePayload
			payload.Usage.Rolling, payload.Usage.Weekly = &rolling, &weekly
			return payload, nil
		}
	}
	h := newBridge([]account{acc}, "example", run, usageFor, testCLI(t), "", false)
	for _, tt := range []struct {
		name, auth, body string
		status           int
		want             string
	}{
		{"auth", "", `{}`, 401, "invalid bridge token"},
		{"tools", "Bearer " + testToken, `{"model":"example","tools":[]}`, 400, "plain-text"},
		{"wrong model", "Bearer " + testToken, `{"model":"other"}`, 404, "model not available"},
		{"text", "Bearer " + testToken, `{"model":"example","messages":[{"role":"user","content":"ping"}]}`, 200, `"content":"OK"`},
		{"stream", "Bearer " + testToken, `{"model":"example","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"ping"}]}`, 200, "data: [DONE]"},
		{"quota", "Bearer " + testToken, ``, 200, `"weekly"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			method, target := "POST", "/v1/chat/completions"
			if tt.name == "quota" {
				method, target = "GET", "/v1/quota"
			}
			r := httptest.NewRequest(method, target, strings.NewReader(tt.body))
			r.Header.Set("Authorization", tt.auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

// 代理头只在令牌认证通过后生效：没有令牌时带代理头也是 401，
// 有令牌时请求头优先于账号固定代理，并如实传给 CLI 执行器。
func TestProxyHeaderRequiresAuthAndOverridesAccountProxy(t *testing.T) {
	acc := testAccount(t, "alice")
	acc.proxy = "http://account-proxy:1"
	var seen []string
	run := func(_ account, proxy string) runner {
		seen = append(seen, proxy)
		return func(_ context.Context, _ string, emit func(string) error) (usage, error) { return usage{}, emit("OK") }
	}
	usageFor := func(_ account, proxy string) func(context.Context) (usagePayload, error) {
		seen = append(seen, proxy)
		percent := 50.0
		return func(context.Context) (usagePayload, error) {
			var payload usagePayload
			payload.Usage.Weekly = &usageWindow{Percent: &percent}
			return payload, nil
		}
	}
	h := newBridge([]account{acc}, "example", run, usageFor, testCLI(t), "", false)

	r := httptest.NewRequest("GET", "/v1/quota", nil)
	r.Header.Set(proxyHeader, "http://other:2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || len(seen) != 0 {
		t.Fatalf("unauthenticated proxy header reached the runner: %d %v", w.Code, seen)
	}

	r = httptest.NewRequest("GET", "/v1/quota", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set(proxyHeader, "http://other:2")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || len(seen) != 1 || seen[0] != "http://other:2" {
		t.Fatalf("%d proxies=%v, want request header to win", w.Code, seen)
	}

	r = httptest.NewRequest("GET", "/v1/quota", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || seen[len(seen)-1] != "http://account-proxy:1" {
		t.Fatalf("account proxy fallback = %v", seen)
	}
	// 代理地址不得出现在任何响应里（可能含账号密码）。
	if strings.Contains(w.Body.String(), "proxy") {
		t.Fatalf("proxy leaked into response: %s", w.Body.String())
	}
}

func TestStreamDoesNotEndSuccessfullyAfterFailure(t *testing.T) {
	acc := testAccount(t, "alice")
	run := func(account, string) runner {
		return func(_ context.Context, _ string, emit func(string) error) (usage, error) {
			_ = emit("partial")
			return usage{}, errors.New("private detail")
		}
	}
	usageFor := func(account, string) func(context.Context) (usagePayload, error) {
		return func(context.Context) (usagePayload, error) { return usagePayload{}, context.Canceled }
	}
	h := newBridge([]account{acc}, "example", run, usageFor, testCLI(t), "", false)
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"example","stream":true,"messages":[{"role":"user","content":"ping"}]}`))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "agy did not finish") || strings.Contains(body, "[DONE]") || strings.Contains(body, "private detail") {
		t.Fatal(body)
	}
}

func TestReadEvents(t *testing.T) {
	for _, tt := range []struct {
		data, want string
		bad        bool
	}{
		{`{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"hi"}}` + "\n" + `{"event":"result","result":{"status":"SUCCESS","response":"hi"}}`, "hi", false},
		{`{"event":"result","result":{"status":"SUCCESS","response":"hi"}}`, "hi", false},
		{`{"event":"result","result":{"status":"ERROR","response":"private"}}`, "", true},
		{`{"event":"init"}`, "", true},
	} {
		var result strings.Builder
		_, err := readEvents(strings.NewReader(tt.data), func(s string) error { result.WriteString(s); return nil })
		if (err != nil) != tt.bad || result.String() != tt.want {
			t.Fatalf("%q: %q / %v", tt.data, result.String(), err)
		}
	}
}
