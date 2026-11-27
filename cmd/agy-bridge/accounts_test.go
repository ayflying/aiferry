package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyEnv(t *testing.T) {
	for _, tc := range []struct {
		proxy string
		want  int
		bad   bool
	}{
		{"", 0, false},
		{"http://user:pass@127.0.0.1:7890", 6, false},
		{"socks5h://10.0.0.1:1080", 6, false},
		{"ftp://10.0.0.1:21", 0, true},
		{"not a url", 0, true},
	} {
		env, err := proxyEnv(tc.proxy)
		if (err != nil) != tc.bad || len(env) != tc.want {
			t.Fatalf("proxyEnv(%q) = %v / %v, want %d vars bad=%t", tc.proxy, env, err, tc.want, tc.bad)
		}
	}
	// 本机回环地址永远不走代理，否则桥接自身的健康检查也会被代理拦截。
	env, _ := proxyEnv("http://10.0.0.1:7890")
	if !contains(env, "NO_PROXY=localhost,127.0.0.1,::1") {
		t.Fatalf("NO_PROXY missing: %v", env)
	}
}

func TestBaseEnvIsolatesHostEnvironment(t *testing.T) {
	t.Setenv("HOME", "/host/home")
	t.Setenv("AGY_BRIDGE_TOKEN_FILE", "/host/secret")
	t.Setenv("TZ", "Asia/Shanghai")
	env := baseEnv("/accounts/alice/home")
	if !contains(env, "HOME=/accounts/alice/home") || !contains(env, "TZ=Asia/Shanghai") {
		t.Fatalf("env = %v", env)
	}
	for _, item := range env {
		if strings.HasPrefix(item, "AGY_BRIDGE") {
			t.Fatalf("host variable leaked into CLI environment: %s", item)
		}
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestLoadAccounts(t *testing.T) {
	root := t.TempDir()
	// 旧部署的单账号写法必须继续可用，HOME 指向独立目录而不是 stateDir 本身。
	if err := writeFile(root+"/bridge.token", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	accounts, err := loadAccounts(root, false)
	if err != nil || len(accounts) != 1 || accounts[0].name != "default" {
		t.Fatalf("single-account load = %+v / %v", accounts, err)
	}
	// 多账号写法：每个子目录一个账号；弱令牌目录整批拒绝，避免带病启动。
	for name, token := range map[string]string{"alice": strings.Repeat("b", 64), "bob-2": strings.Repeat("c", 64), "weak": "short"} {
		if err := writeFile(root+"/accounts/"+name+"/token", token); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadAccounts(root, false); err == nil || !strings.Contains(err.Error(), "weak") {
		t.Fatalf("weak token accepted: %v", err)
	}
	if err := writeFile(root+"/accounts/weak/token", strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	// 非法目录名与空令牌目录被忽略，不参与认证。
	_ = writeFile(root+"/accounts/../escape/token", strings.Repeat("e", 64))
	_ = writeFile(root+"/accounts/empty/token", "")
	accounts, err = loadAccounts(root, false)
	if err != nil || len(accounts) != 4 {
		t.Fatalf("accounts = %+v / %v", accounts, err)
	}
	// 请求头里的令牌能精确落到对应账号，错令牌落到空账号。
	if acc, ok := resolve(accounts, "Bearer "+strings.Repeat("c", 64)); !ok || acc.name != "bob-2" {
		t.Fatalf("resolve = %+v / %t", acc, ok)
	}
	if _, ok := resolve(accounts, "Bearer nope"); ok {
		t.Fatal("invalid token resolved")
	}
}

func TestUsageOutputParsing(t *testing.T) {
	// 取自官方 CLI 1.5.18 的真实 command_result 事件（数值已截短）。
	stream := `{"event":"init"}` + "\n" +
		`{"event":"command_result","command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-weekly","window":"weekly","remaining_fraction":0.8381,"reset_time":"2026-12-04T02:46:35Z"},{"id":"gemini-5h","window":"5h","remaining_fraction":0.3679,"reset_time":"2026-11-27T07:46:35Z"}]},{"name":"Claude and GPT models","buckets":[{"id":"3p-weekly","window":"weekly","remaining_fraction":1,"reset_time":"1970-01-01T00:00:00Z"}]}]}}}` + "\n" +
		`{"event":"result","result":{"status":"SUCCESS"}}` + "\n"
	payload, err := parseUsageOutput(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if payload.Usage.Rolling == nil || payload.Usage.Weekly == nil {
		t.Fatalf("windows = %+v", payload.Usage)
	}
	// 剩余比例换算成「已用百分比」：5 小时 63.21%，每周 16.19%。
	if got := *payload.Usage.Rolling.Percent; got < 63.2 || got > 63.21 {
		t.Fatalf("rolling percent = %v", got)
	}
	if got := *payload.Usage.Weekly.Percent; got < 16.18 || got > 16.2 {
		t.Fatalf("weekly percent = %v", got)
	}
	if payload.Usage.Rolling.ResetsAt != "2026-11-27T07:46:35Z" || payload.Usage.Weekly.ResetsAt != "2026-12-04T02:46:35Z" {
		t.Fatalf("reset times = %s / %s", payload.Usage.Rolling.ResetsAt, payload.Usage.Weekly.ResetsAt)
	}
	// 第三方分组不参与：只有 3p 分组时视为没有可用额度，报错而不是返回空视图。
	if _, err := parseUsageOutput(strings.NewReader(`{"event":"command_result","command":{"data":{"groups":[{"name":"Claude and GPT models","buckets":[{"id":"3p-5h","window":"5h","remaining_fraction":1}]}]}}}`)); err == nil {
		t.Fatal("third-party-only result accepted")
	}
	// 没有结构化结果（模型自由回答）不能当额度。
	if _, err := parseUsageOutput(strings.NewReader("Antigravity has no /usage command.\n")); err == nil {
		t.Fatal("free text accepted as usage")
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}
