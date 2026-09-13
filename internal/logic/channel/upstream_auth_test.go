package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

// newUpstreamAuthTestService 构造带内置渠道类型表与真实加解密能力的渠道服务，
// 用于验证正式转发与模型测试共用的上游鉴权规则。
func newUpstreamAuthTestService(t *testing.T, channelTypeJSON string) (*sChannel, *secret.Service) {
	t.Helper()
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	builtins := &config.BuiltinRegistry{ChannelTypes: []config.BuiltinChannelType{{
		ID: 77, Name: "AuthFixture", Code: "auth_fixture", Config: json.RawMessage(channelTypeJSON),
	}}}
	return &sChannel{
		app:   &app.Service{Secrets: secrets},
		types: channeltype.New(builtins, nil),
	}, secrets
}

func TestUpstreamAuthSpecForChannelTypeReadsTypeConfig(t *testing.T) {
	svc, _ := newUpstreamAuthTestService(t, `{"models":{"path":"/models","idPath":"id","authType":"management_key","headerName":"x-api-key","headerPrefix":"Api-Key "},"costs":{"adapter":"none"}}`)
	spec, err := svc.UpstreamAuthSpecForChannelType(context.Background(), "auth_fixture")
	if err != nil {
		t.Fatal(err)
	}
	if spec.AuthType != channeltype.AuthManagementKey || spec.HeaderName != "x-api-key" || spec.HeaderPrefix != "Api-Key " {
		t.Fatalf("unexpected auth spec: %+v", spec)
	}
	// 未知类型解析失败应报错，交由调用方回退默认规则。
	if _, err = svc.UpstreamAuthSpecForChannelType(context.Background(), "not_exists"); err == nil {
		t.Fatal("expected error for unknown channel type")
	}
}

func TestApplyUpstreamAuthHeadersUsesChannelKey(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{"models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`)
	cipher, err := secrets.Encrypt("sk-channel-key")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := svc.UpstreamAuthSpecForChannelType(context.Background(), "auth_fixture")
	if err != nil {
		t.Fatal(err)
	}
	if spec.AuthType != channeltype.AuthChannelKey || spec.HeaderName != "Authorization" || spec.HeaderPrefix != "Bearer " {
		t.Fatalf("built-in models should default to Authorization/Bearer: %+v", spec)
	}
	req, _ := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	if err = svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{
		Spec:             spec,
		CredentialCipher: cipher,
		OrganizationID:   "org-1",
		ProjectID:        "proj-1",
	}); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-channel-key" {
		t.Fatalf("unexpected authorization header: %q", got)
	}
	if req.Header.Get("Accept") != "application/json" {
		t.Fatalf("missing default accept header: %v", req.Header)
	}
	if req.Header.Get("OpenAI-Organization") != "org-1" || req.Header.Get("OpenAI-Project") != "proj-1" {
		t.Fatalf("organization/project headers were not applied: %v", req.Header)
	}
}

func TestApplyUpstreamAuthHeadersHonorsCustomHeaderAndManagementKey(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{"models":{"path":"/models","idPath":"id","authType":"management_key","headerName":"x-api-key","headerPrefix":""},"costs":{"adapter":"none"}}`)
	cipher, err := secrets.Encrypt("mgmt-key")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := svc.UpstreamAuthSpecForChannelType(context.Background(), "auth_fixture")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://upstream.example/v1/models", nil)
	if err = svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: spec, ManagementKeyCipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("x-api-key") != "mgmt-key" || req.Header.Get("Authorization") != "" {
		t.Fatalf("management key should use the declared header: %v", req.Header)
	}
	// 管理密钥缺失必须报错（测试/同步链路），转发链路则可容忍。
	strict, _ := http.NewRequest(http.MethodGet, "https://upstream.example/v1/models", nil)
	if err = svc.ApplyUpstreamAuthHeaders(strict, UpstreamAuthInput{Spec: spec}); err == nil {
		t.Fatal("missing management key should fail fast")
	}
	tolerant, _ := http.NewRequest(http.MethodGet, "https://upstream.example/v1/models", nil)
	if err = svc.ApplyUpstreamAuthHeaders(tolerant, UpstreamAuthInput{Spec: spec, TolerateMissingKey: true}); err != nil {
		t.Fatal(err)
	}
	if tolerant.Header.Get("x-api-key") != "" {
		t.Fatalf("empty key should not set a header: %v", tolerant.Header)
	}
}

func TestApplyUpstreamAuthHeadersKeepsClientAcceptAndSkipsAuthNone(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{"models":{"path":"/models","idPath":"id","authType":"none"},"costs":{"adapter":"none"}}`)
	cipher, err := secrets.Encrypt("sk-ignored")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := svc.UpstreamAuthSpecForChannelType(context.Background(), "auth_fixture")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	req.Header.Set("Accept", "text/event-stream")
	if err = svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: spec, CredentialCipher: cipher}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("client accept header must be preserved: %v", req.Header)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("authType=none must not send credentials: %v", req.Header)
	}
	// 缺少密钥时认证型渠道应报错，转发链路则容忍空密钥。
	channelKey, _ := newUpstreamAuthTestService(t, `{"models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`)
	keySpec, err := channelKey.UpstreamAuthSpecForChannelType(context.Background(), "auth_fixture")
	if err != nil {
		t.Fatal(err)
	}
	strict, _ := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	if err = channelKey.ApplyUpstreamAuthHeaders(strict, UpstreamAuthInput{Spec: keySpec}); err == nil {
		t.Fatal("missing channel credential should fail fast")
	}
	tolerant, _ := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	if err = channelKey.ApplyUpstreamAuthHeaders(tolerant, UpstreamAuthInput{Spec: keySpec, TolerateMissingKey: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultUpstreamAuthSpecIsBearerChannelKey(t *testing.T) {
	spec := DefaultUpstreamAuthSpec()
	if spec.AuthType != channeltype.AuthChannelKey || spec.HeaderName != "Authorization" || spec.HeaderPrefix != "Bearer " {
		t.Fatalf("unexpected default auth spec: %+v", spec)
	}
	// 空类型编码时直接返回默认规则，避免转发因类型缺失中断。
	svc, _ := newUpstreamAuthTestService(t, `{"models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`)
	resolved, err := svc.UpstreamAuthSpecForChannelType(context.Background(), "  ")
	if err != nil || resolved != spec {
		t.Fatalf("blank channel type should fall back to default: %+v err=%v", resolved, err)
	}
}
