package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// TestLoginEndpointUsesHostRoot 确认登录地址按「根地址的 scheme+host」重建：
// WorkBuddy 的渠道根地址带 /v2 版本前缀，而登录接口固定在 /v2 之下，
// 直接拼接会得到 /v2/v2/plugin/... 这类错地址。
func TestLoginEndpointUsesHostRoot(t *testing.T) {
	config := channeltype.Config{Login: channeltype.LoginConfig{
		Adapter: channeltype.AdapterLoginExternalLink, Platform: "workbuddy", PrefixPath: "/plugin",
	}}
	endpoint, err := loginEndpoint("https://www.workbuddy.cn/v2", config, "auth/state")
	if err != nil {
		t.Fatalf("loginEndpoint: %v", err)
	}
	if want := "https://www.workbuddy.cn/v2/plugin/auth/state"; endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

// TestLoginEndpointRejectsInvalidBaseURL 覆盖根地址不可用的分支。
func TestLoginEndpointRejectsInvalidBaseURL(t *testing.T) {
	config := channeltype.Config{Login: channeltype.LoginConfig{PrefixPath: "/plugin"}}
	if _, err := loginEndpoint("not-a-url", config, "auth/state"); err == nil {
		t.Fatal("loginEndpoint must reject a non-URL base address")
	}
}