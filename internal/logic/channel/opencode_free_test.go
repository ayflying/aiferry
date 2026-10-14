package channel

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 指纹模拟只认渠道类型 opencode_zen：地址再像免费层也不行，
// opencode_go 与其它类型一律不模拟编辑器。
func TestIsOpenCodeFreeLaneOnlyZenType(t *testing.T) {
	if !IsOpenCodeFreeLane(OpenCodeZenChannelType, "") {
		t.Fatal("opencode_zen type must hit free lane with empty URL")
	}
	if !IsOpenCodeFreeLane(OpenCodeZenChannelType, "https://example.com/whatever") {
		t.Fatal("opencode_zen type must hit free lane regardless of URL")
	}
	// 地址兜底已移除：免费层地址也不足以触发模拟。
	notFree := []struct {
		channelType string
		baseURL     string
	}{
		{"", "https://opencode.ai/zen/v1"},
		{"", "https://opencode.ai/zen/v1/"},
		{"", "HTTPS://OpenCode.AI/Zen/V1"},
		{"", "  https://opencode.ai/zen/v1  "},
		{OpenCodeGoChannelType, "https://opencode.ai/zen/go/v1"},
		{OpenCodeGoChannelType, "https://opencode.ai/zen/v1"},
		{"openai", "https://opencode.ai/zen/v1"},
		{"", ""},
		{"openai", ""},
	}
	for _, item := range notFree {
		if IsOpenCodeFreeLane(item.channelType, item.baseURL) {
			t.Fatalf("type=%q url=%q must not hit free lane", item.channelType, item.baseURL)
		}
	}
}

// opencode_zen：注入官方 CLI 同款指纹（opencode/ UA、会话三处同值、
// client/request/project 标识），且派生会话跨请求稳定。
func TestApplyUpstreamClientHeadersFreeLaneInjectsFingerprint(t *testing.T) {
	identity := UpstreamClientIdentity{
		ChannelType:  OpenCodeZenChannelType,
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
		t.Fatalf("expected canonical ses_ session, got %q", session)
	}
	if got := target.Get("X-Opencode-Client"); got != "cli" {
		t.Fatalf("expected x-opencode-client=cli, got %q", got)
	}
	for _, name := range []string{"X-Session-Id", "X-Session-Affinity"} {
		if got := target.Get(name); got != session {
			t.Fatalf("expected %s to mirror the session, got %q", name, got)
		}
	}
	if got := target.Get("X-Opencode-Request"); !strings.HasPrefix(got, "req_") {
		t.Fatalf("expected req_ request id, got %q", got)
	}
	if got := target.Get("X-Opencode-Project"); !strings.HasPrefix(got, "prj_") {
		t.Fatalf("expected prj_ project id, got %q", got)
	}

	second := http.Header{}
	ApplyUpstreamClientHeaders(second, nil, identity)
	if second.Get(openCodeSessionHeader) != session {
		t.Fatalf("expected stable free-lane session, got %q and %q", session, second.Get(openCodeSessionHeader))
	}
}

// 客户端自带合规 ses_ 会话（形状与计数器窗口均合法）与 opencode/ UA
// 时原样透传。
func TestApplyUpstreamClientHeadersFreeLanePassesThroughClientSession(t *testing.T) {
	identity := UpstreamClientIdentity{
		ChannelType:  OpenCodeZenChannelType,
		ChannelID:    7,
		CredentialID: 11,
		ModelName:    "grok-code-fast-1",
		BaseURL:      "https://opencode.ai/zen/v1",
	}
	// ses_ + 官方客户端同款反码时间戳 hex（counter=1）+ 14 位 Base62。
	canonical := openCodeSessionPrefix + invertedTimestampHex(time.Now().UnixMilli(), 1) + "CDEFGHIJKLMN12"
	incoming := http.Header{}
	incoming.Set("x-opencode-session", canonical)
	incoming.Set("User-Agent", "opencode/1.18.31")
	target := http.Header{}
	ApplyUpstreamClientHeaders(target, incoming, identity)
	if got := target.Get(openCodeSessionHeader); got != canonical {
		t.Fatalf("expected compliant client session passthrough, got %q", got)
	}
	if got := target.Get("User-Agent"); got != "opencode/1.18.31" {
		t.Fatalf("expected opencode UA passthrough, got %q", got)
	}
}

// opencode_go 即使地址指向免费层也不模拟编辑器：走 aiferry 会话，不发 ses_。
func TestApplyUpstreamClientHeadersGoLaneNeverSimulates(t *testing.T) {
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
	if session == "" {
		t.Fatal("opencode_go must still set x-opencode-session for upstream routing")
	}
	if isOpenCodeFreeSessionID(session) {
		t.Fatalf("opencode_go must not use free-lane ses_ session, got %q", session)
	}
	if !strings.HasPrefix(session, "aiferry-") {
		t.Fatalf("expected aiferry- session for opencode_go, got %q", session)
	}
	if ua := target.Get("User-Agent"); strings.HasPrefix(ua, "opencode/") {
		t.Fatalf("opencode_go must not force opencode/ UA, got %q", ua)
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
	// hex 段必须能反向解码出合法计数器，且 counter=1 时低 12 位按位取反
	// 后结构性落在 ffe（上游解码窗口 [0,63] 的来源）。
	clockHex := session[len(openCodeSessionPrefix):][:12]
	inverted, err := strconv.ParseUint(clockHex, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	if counter := ^inverted & openCodeCounterMask; counter > openCodeCounterMax {
		t.Fatalf("derived session counter %d outside accepted window [0,%d]", counter, openCodeCounterMax)
	}
	if !strings.HasSuffix(clockHex, "ffe") {
		t.Fatalf("expected counter-1 encoding to end in ffe, got %q", clockHex)
	}
	// 与官方编码器的行为逐位对齐：纪元毫秒 + counter=1 必须得到
	// fffffffffffe（~1 截断 48 位）。
	if got := invertedTimestampHex(0, 1); got != "fffffffffffe" {
		t.Fatalf("epoch mint mismatch: %q", got)
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
		// 形状完全合规，但 hex 段按位取反解码出 counter=1620，超出上游
		// [0,63] 窗口——摘要式会话即使形状正确也几乎必 403。
		"ses_0123456789abCDEFGHIJKLMN12",
	}
	for _, value := range invalid {
		if isOpenCodeFreeSessionID(value) {
			t.Fatalf("expected %q rejected", value)
		}
	}
}
