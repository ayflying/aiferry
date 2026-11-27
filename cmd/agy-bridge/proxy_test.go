package main

import (
	"strings"
	"testing"
)

// TestGlobalProxyPriority 验证全局代理的读取顺序：桥接专用变量优先，其次标准
// HTTPS_PROXY / HTTP_PROXY；都没配即空串（直连）。
func TestGlobalProxyPriority(t *testing.T) {
	for _, name := range []string{"AGY_BRIDGE_PROXY", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(name, "")
	}
	if got := globalProxy(); got != "" {
		t.Fatalf("no config: want empty, got %q", got)
	}
	t.Setenv("HTTP_PROXY", "http://fallback:1")
	if got := globalProxy(); got != "http://fallback:1" {
		t.Fatalf("http fallback: got %q", got)
	}
	t.Setenv("HTTPS_PROXY", "http://secure:2")
	if got := globalProxy(); got != "http://secure:2" {
		t.Fatalf("https should beat http: got %q", got)
	}
	t.Setenv("AGY_BRIDGE_PROXY", "http://dedicated:3")
	if got := globalProxy(); got != "http://dedicated:3" {
		t.Fatalf("dedicated should beat all: got %q", got)
	}
}

// TestProxyEnvInheritsNoProxy 验证编排里配置的内网例外会并入 agy 进程的 NO_PROXY，
// 同时回环地址始终保留——否则主服务连内网 MySQL/Redis、容器内健康检查都会被代理拦下。
func TestProxyEnvInheritsNoProxy(t *testing.T) {
	t.Setenv("NO_PROXY", "aiferry,10.0.0.0/8")
	env, err := proxyEnv("http://10.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	want := "NO_PROXY=localhost,127.0.0.1,::1,aiferry,10.0.0.0/8"
	if !contains(env, want) || !contains(env, "no_proxy="+strings.TrimPrefix(want, "NO_PROXY=")) {
		t.Fatalf("NO_PROXY not inherited: %v", env)
	}
	// 没配 NO_PROXY 时只有回环地址，不能把宿主的空值拼成多余的逗号。
	t.Setenv("NO_PROXY", "")
	env, _ = proxyEnv("http://10.0.0.1:7890")
	if !contains(env, "NO_PROXY=localhost,127.0.0.1,::1") {
		t.Fatalf("bare NO_PROXY wrong: %v", env)
	}
}
