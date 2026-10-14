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

// 2026-10-14 起匿名推理端点从 …/zen 迁到 https://opencode.ai/inference：
// chat 请求基址必须改写并补 /v1 前缀；绝对端点与非 opencode.ai 基址
// （本地 mock 联调）原样保留。
func TestOpenCodeFreeUpstreamURLRewritesAnonymousBase(t *testing.T) {
	cases := []struct {
		baseURL  string
		endpoint string
		want     string
	}{
		{"https://opencode.ai/zen/v1", "/chat/completions", "https://opencode.ai/inference/v1/chat/completions"},
		{"https://opencode.ai/zen", "/v1/chat/completions", "https://opencode.ai/inference/v1/chat/completions"},
		{"https://opencode.ai/zen/v1/", "/chat/completions", "https://opencode.ai/inference/v1/chat/completions"},
		{"https://opencode.ai/zen/v1", "/messages", "https://opencode.ai/inference/v1/messages"},
		{"HTTPS://OpenCode.AI/Zen/V1", "/chat/completions", "https://opencode.ai/inference/v1/chat/completions"},
		{"https://opencode.ai/zen/v1", "https://mock.local/v1/chat/completions", "https://mock.local/v1/chat/completions"},
		{"http://127.0.0.1:9911/v1", "/chat/completions", "http://127.0.0.1:9911/v1/chat/completions"},
	}
	for _, item := range cases {
		if got := OpenCodeFreeUpstreamURL(item.baseURL, item.endpoint); got != item.want {
			t.Fatalf("OpenCodeFreeUpstreamURL(%q, %q) = %q, want %q", item.baseURL, item.endpoint, got, item.want)
		}
	}
}

// opencode_zen：注入官方 2.x 客户端同款指纹（opencode/latest/<版本>/cli
// UA、会话四处同值、client/project 标识；2026-10-14 起上游按 UA 形状
// 拒绝，不再透传客户端 UA；不再发送 x-opencode-request），且派生会话
// 跨请求稳定。
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

	if got := target.Get("User-Agent"); got != openCodeFreeUserAgent() {
		t.Fatalf("expected forced %q UA, got %q", openCodeFreeUserAgent(), got)
	}
	session := target.Get(openCodeSessionHeader)
	if !isOpenCodeFreeSessionID(session) {
		t.Fatalf("expected canonical ses_ session, got %q", session)
	}
	if got := target.Get("X-Opencode-Client"); got != "cli" {
		t.Fatalf("expected x-opencode-client=cli, got %q", got)
	}
	for _, name := range []string{"X-Opencode-Session-Id", "X-Session-Id", "X-Session-Affinity"} {
		if got := target.Get(name); got != session {
			t.Fatalf("expected %s to mirror the session, got %q", name, got)
		}
	}
	if got := target.Get("X-Opencode-Request"); got != "" {
		t.Fatalf("expected x-opencode-request to be absent (2026-10-14 gate), got %q", got)
	}
	project := target.Get("X-Opencode-Project")
	if len(project) != 40 {
		t.Fatalf("expected bare 40-char project id, got %q", project)
	}
	for i := 0; i < len(project); i++ {
		c := project[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("expected lowercase hex project id, got %q", project)
		}
	}
	if strings.HasPrefix(project, "prj_") {
		t.Fatalf("expected prj_ prefix removed (2026-10-14 gate), got %q", project)
	}

	second := http.Header{}
	ApplyUpstreamClientHeaders(second, nil, identity)
	if second.Get(openCodeSessionHeader) != session {
		t.Fatalf("expected stable free-lane session, got %q and %q", session, second.Get(openCodeSessionHeader))
	}
}

// 客户端自带合规 ses_ 会话（形状与计数器窗口均合法）时原样透传保住
// 会话亲和；UA 一律改写为官方 2.x 形状。
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
	incoming.Set("User-Agent", "opencode/1.18.31 (windows amd64; node22.21.1)")
	target := http.Header{}
	ApplyUpstreamClientHeaders(target, incoming, identity)
	if got := target.Get(openCodeSessionHeader); got != canonical {
		t.Fatalf("expected compliant client session passthrough, got %q", got)
	}
	if got := target.Get("User-Agent"); got != openCodeFreeUserAgent() {
		t.Fatalf("expected legacy opencode UA rewritten to %q, got %q", openCodeFreeUserAgent(), got)
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

// 无工具请求注入全部核心工具，且定义必须来自官方真实抓取（桩定义会被
// 2026-10-14 门禁 403）；tool_choice:none 同样会被拒绝，必须剥掉。
func TestApplyOpenCodeFreeBodyInjectsGenuineCoreTools(t *testing.T) {
	original := []byte(`{"model":"grok-code-fast-1","messages":[{"role":"user","content":"hi"}],"tool_choice":"none"}`)
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
	if _, exists := payload["tool_choice"]; exists {
		t.Fatalf("expected tool_choice=none stripped (2026-10-14 gate), got %v", payload["tool_choice"])
	}
	tools, _ := payload["tools"].([]any)
	names := map[string]map[string]any{}
	for _, item := range tools {
		object, _ := item.(map[string]any)
		function, _ := object["function"].(map[string]any)
		name, _ := function["name"].(string)
		names[name] = function
	}
	if len(tools) != len(openCodeCoreTools) {
		t.Fatalf("expected %d core tools, got %d", len(openCodeCoreTools), len(tools))
	}
	for _, name := range openCodeCoreTools {
		function, ok := names[name]
		if !ok {
			t.Fatalf("expected core tool %q injected, got %v", name, names)
		}
		definition, known := knownOpencodeTool(name)
		if !known {
			t.Fatalf("official definition for %q must be embedded", name)
		}
		if got, _ := function["description"].(string); got != definition.Description {
			t.Fatalf("expected genuine description for %q, got %.80q", name, got)
		}
		parameters, _ := function["parameters"].(map[string]any)
		properties, _ := parameters["properties"].(map[string]any)
		if len(properties) != len(definition.Parameters["properties"].(map[string]any)) {
			t.Fatalf("expected genuine schema for %q, got %v", name, parameters)
		}
	}
	options, _ := payload["stream_options"].(map[string]any)
	if include, _ := options["include_usage"].(bool); !include {
		t.Fatal("expected stream_options.include_usage=true")
	}
}

// 下游声明的官方同名工具改写为真实定义、未知工具原样保留、
// tool_choice:auto 保留。
func TestApplyOpenCodeFreeBodyRewritesRecognizedToolsKeepsForeign(t *testing.T) {
	original := []byte(`{"model":"m","stream":true,"tool_choice":"auto","tools":[` +
		`{"type":"function","function":{"name":"read","description":"stub","parameters":{"type":"object","properties":{}}}},` +
		`{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]}`)
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
	if choice, _ := payload["tool_choice"].(string); choice != "auto" {
		t.Fatalf("expected tool_choice=auto preserved, got %v", payload["tool_choice"])
	}
	tools, _ := payload["tools"].([]any)
	// read（改写）+ get_weather（保留）+ 补齐 shell/edit/glob/grep 共 6 个。
	if len(tools) != len(openCodeCoreTools)+1 {
		t.Fatalf("expected %d tools, got %d", len(openCodeCoreTools)+1, len(tools))
	}
	definition, _ := knownOpencodeTool("read")
	foundGenuineRead, foundWeather := false, false
	for _, item := range tools {
		object, _ := item.(map[string]any)
		function, _ := object["function"].(map[string]any)
		name, _ := function["name"].(string)
		switch name {
		case "read":
			if got, _ := function["description"].(string); got == definition.Description {
				foundGenuineRead = true
			}
		case "get_weather":
			if got, _ := function["description"].(string); got == "Get weather" {
				foundWeather = true
			}
		}
	}
	if !foundGenuineRead {
		t.Fatal("expected downstream read stub rewritten to genuine definition")
	}
	if !foundWeather {
		t.Fatal("expected foreign get_weather tool preserved verbatim")
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
