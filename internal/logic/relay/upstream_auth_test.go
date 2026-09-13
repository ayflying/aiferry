package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

// TestApplyUpstreamAuthHeadersFollowsChannelType 验证正式转发不再硬编码
// Authorization: Bearer，而是与模型测试/同步共用渠道类型声明的鉴权规则。
func TestApplyUpstreamAuthHeadersFollowsChannelType(t *testing.T) {
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	builtins := &config.BuiltinRegistry{ChannelTypes: []config.BuiltinChannelType{{
		ID: 99, Name: "Custom", Code: "custom_openai",
		Config: json.RawMessage(`{"models":{"path":"/models","idPath":"id","authType":"channel_key","headerName":"x-api-key","headerPrefix":""},"costs":{"adapter":"none"}}`),
	}}}
	relay := &sRelay{channels: channel.New(&app.Service{Secrets: secrets}, channeltype.New(builtins, nil), nil, nil, nil, nil, nil, nil)}
	cipher, err := secrets.Encrypt("sk-relay-key")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	if err = relay.applyUpstreamAuthHeaders(context.Background(), req, Candidate{ChannelType: "custom_openai", APIKeyCipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("x-api-key") != "sk-relay-key" || req.Header.Get("Authorization") != "" {
		t.Fatalf("forwarding should follow the declared auth header: %v", req.Header)
	}

	// 渠道类型解析失败时回退标准 Bearer，转发不中断。
	fallback, _ := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	if err = relay.applyUpstreamAuthHeaders(context.Background(), fallback, Candidate{ChannelType: "not_exists", APIKeyCipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if fallback.Header.Get("Authorization") != "Bearer sk-relay-key" {
		t.Fatalf("unknown channel type should fall back to bearer: %v", fallback.Header)
	}

	// 无密钥（如已签名下载地址）不应中断转发。
	empty, _ := http.NewRequest(http.MethodGet, "https://cdn.example/v1/videos/1/content", nil)
	if err = relay.applyUpstreamAuthHeaders(context.Background(), empty, Candidate{ChannelType: "custom_openai"}); err != nil {
		t.Fatal(err)
	}
	if empty.Header.Get("x-api-key") != "" {
		t.Fatalf("missing credential should not set an auth header: %v", empty.Header)
	}
}
