package channel

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestIsOpenCodeFreeLaneDistinguishesLanes(t *testing.T) {
	free := []string{
		"https://opencode.ai/zen/v1",
		"https://opencode.ai/zen/v1/",
		"HTTPS://OpenCode.AI/Zen/V1",
		"  https://opencode.ai/zen/v1  ",
		"https://opencode.ai/api/zen/v1/chat/completions",
	}
	for _, url := range free {
		if !IsOpenCodeFreeLane("", url) {
			t.Fatalf("expected free lane for %q", url)
		}
	}
	notFree := []string{
		"",
		"https://opencode.ai/zen/go/v1",
		"https://opencode.ai/zen/go/v1/chat/completions",
		"https://api.openai.com/v1",
		"https://opencode.ai/zen",
	}
	for _, url := range notFree {
		if IsOpenCodeFreeLane("", url) {
			t.Fatalf("expected non-free lane for %q", url)
		}
	}
}

func TestIsOpenCodeFreeLaneChannelTypeWins(t *testing.T) {
	// 渠道类型 opencode_zen 是主判据：即便地址被改写（或为空），只要类型
	// 命中就走免费层指纹；其他类型即使地址为空也绝不误判。
	if !IsOpenCodeFreeLane(OpenCodeZenChannelType, "") {
		t.Fatal("expected opencode_zen type with empty URL to hit free lane")
	}
	if !IsOpenCodeFreeLane(OpenCodeZenChannelType, "https://example.com/whatever") {
		t.Fatal("expected opencode_zen type to hit free lane regardless of URL")
	}
	if IsOpenCodeFreeLane(OpenCodeGoChannelType, "https://opencode.ai/zen/go/v1") {
		t.Fatal("expected opencode_go go-lane URL to stay non-free")
	}
	if IsOpenCodeFreeLane("openai", "") {
		t.Fatal("expected unrelated type with empty URL to stay non-free")
	}
}

func TestApplyUpstreamClientHeadersFreeLaneInjectsFingerprint(t *testing.T) {
	identity := UpstreamClientIdentity{
		ChannelType:  OpenCodeGoChannelType,
		ChannelID:    7,
		CredentialID: 11,
		ModelName:    "grok-code-fast-1",
		BaseURL:      "https://opencode.ai/zen/v1",
	}
	target := http.Header{}
	ApplyUpstreamClientHeaders(target, http.Header{}, identity)

	ua := target.Get("User-Agent")
	if !strings.HasPrefix(ua, "opencode/") {
		t.Fatalf("expected opencode/ UA on free lane, got %q", ua)
	}
	session := target.Get(openCodeSessionHeader)
	if !isOpenCodeFreeSessionID(session) {
		t.Fatalf("expected ses_ session with 12hex+14base62, got %q", session)
	}

	// 派生会话必须稳定：同身份两次注入结果一致。
	second := http.Header{}
	ApplyUpstreamClientHeaders(second, nil, identity)
	if second.Get(openCodeSessionHeader) != session {
		t.Fatalf("expected stable free-lane session, got %q and %q", session, second.Get(openCodeSessionHeader))
	}
}

func TestApplyUpstreamClientHeadersFreeLanePassesThroughClientSession(t *testing.T) {
	identity := UpstreamClientIdentity{
		ChannelID:    7,
		CredentialID: 11,
		ModelName:    "grok-code-fast-1",
		BaseURL:      "https://opencode.ai/zen/v1",
	}
	incoming := http.Header{}
	// ses_ + 12 位 hex + 14 位 Base62，共 26 字符 body。
	incoming.Set("x-opencode-session", "ses_0123456789abCDEFGHIJKLMN12")
	incoming.Set("User-Agent", "opencode/1.18.31")
	target := http.Header{}
	ApplyUpstreamClientHeaders(target, incoming, identity)
	if got := target.Get(openCodeSessionHeader); got != "ses_0123456789abCDEFGHIJKLMN12" {
		t.Fatalf("expected compliant client session passthrough, got %q", got)
	}
	if got := target.Get("User-Agent"); got != "opencode/1.18.31" {
		t.Fatalf("expected opencode UA passthrough, got %q", got)
	}
}

func TestApplyUpstreamClientHeadersFreeLaneOverridesGoLaneSession(t *testing.T) {
	// 渠道类型仍是 opencode_go，但地址切到免费层：必须走免费层会话格式，
	// 不能再发 aiferry- 前缀（免费层会 403）。
	identity := UpstreamClientIdentity{
		ChannelType:  OpenCodeGoChannelType,
		ChannelID:    7,
		CredentialID: 11,
		ModelName:    "grok-code-fast-1",
		BaseURL:      "https://opencode.ai/zen/v1",
	}
	target := http.Header{}
	ApplyUpstreamClientHeaders(target, http.Header{}, identity)
	session := target.Get(openCodeSessionHeader)
	if strings.HasPrefix(session, "aiferry-") {
		t.Fatalf("free lane must not use aiferry- session, got %q", session)
	}
	if !isOpenCodeFreeSessionID(session) {
		t.Fatalf("expected free-lane session format, got %q", session)
	}
}

func TestApplyOpenCodeFreeBodyForcesStreamAndTools(t *testing.T) {
	original := []byte(`{"model":"grok-code-fast-1","messages":[{"role":"user","content":"hi"}]}`)
	body, forced, err := ApplyOpenCodeFreeBody(original)
	if err != nil {
		t.Fatal(err)
	}
	if !forced {
		t.Fatal("expected forcedStream for non-stream original")
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if stream, _ := payload["stream"].(bool); !stream {
		t.Fatal("expected stream=true")
	}
	if choice, _ := payload["tool_choice"].(string); choice != "none" {
		t.Fatalf("expected tool_choice=none when original had no tools, got %v", payload["tool_choice"])
	}
	tools, _ := payload["tools"].([]any)
	names := map[string]bool{}
	for _, item := range tools {
		names[openCodeToolName(item)] = true
	}
	if !names["bash"] || !names["read"] {
		t.Fatalf("expected bash and read tool stubs, got %v", names)
	}
	options, _ := payload["stream_options"].(map[string]any)
	if include, _ := options["include_usage"].(bool); !include {
		t.Fatal("expected stream_options.include_usage=true")
	}
}

func TestApplyOpenCodeFreeBodyKeepsExistingToolsAndStream(t *testing.T) {
	original := []byte(`{"model":"m","stream":true,"tool_choice":"auto","tools":[{"type":"function","function":{"name":"bash","parameters":{}}},{"type":"function","function":{"name":"get_weather","parameters":{}}}]}`)
	body, forced, err := ApplyOpenCodeFreeBody(original)
	if err != nil {
		t.Fatal(err)
	}
	if forced {
		t.Fatal("expected forcedStream=false when original already streams")
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	// 原有工具选择策略必须保留。
	if choice, _ := payload["tool_choice"].(string); choice != "auto" {
		t.Fatalf("expected tool_choice=auto preserved, got %v", payload["tool_choice"])
	}
	tools, _ := payload["tools"].([]any)
	// 原 bash 保留、read 补桩、get_weather 保留，共 3 个。
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools (original bash + added read + get_weather), got %d", len(tools))
	}
	names := map[string]bool{}
	for _, item := range tools {
		names[openCodeToolName(item)] = true
	}
	if !names["bash"] || !names["read"] || !names["get_weather"] {
		t.Fatalf("expected bash+read+get_weather, got %v", names)
	}
}

func TestApplyOpenCodeFreeBodyStripsPromptCacheControls(t *testing.T) {
	original := []byte(`{"model":"m","messages":[],"prompt_cache_key":"abc","prompt_cache_options":{"x":1}}`)
	body, _, err := ApplyOpenCodeFreeBody(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "prompt_cache") {
		t.Fatalf("expected prompt cache fields stripped, got %s", body)
	}
}

func TestOpenCodeFreeSessionFormat(t *testing.T) {
	identity := UpstreamClientIdentity{ChannelID: 1, CredentialID: 2, ModelName: "m", UserID: 3}
	session := deriveOpenCodeFreeSessionID(identity)
	if !isOpenCodeFreeSessionID(session) {
		t.Fatalf("derived session must satisfy free-lane format, got %q", session)
	}
	if len(session) != len("ses_")+26 {
		t.Fatalf("expected ses_ + 26 chars, got %q (%d)", session, len(session))
	}
	if deriveOpenCodeFreeSessionID(identity) != session {
		t.Fatal("expected deterministic derivation")
	}
	other := identity
	other.UserID = 4
	if deriveOpenCodeFreeSessionID(other) == session {
		t.Fatal("expected different users to derive different sessions")
	}
	invalid := []string{
		"aiferry-0123456789abcdef",
		"ses_short",
		"ses_0123456789ab!", // 非法字符
		"ses_0123456789ab",  // 长度不足
	}
	for _, value := range invalid {
		if isOpenCodeFreeSessionID(value) {
			t.Fatalf("expected %q rejected", value)
		}
	}
}
