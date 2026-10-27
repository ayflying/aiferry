package channel

import (
	"net/http"
	"regexp"
	"testing"
)

// 官方客户端形态的请求标识：32 位小写十六进制 UUIDv7（版本位 7、变体位 10xx）。
var workBuddyRequestIDPattern = regexp.MustCompile(`^[0-9a-f]{12}7[0-9a-f]{3}[89ab][0-9a-f]{15}$`)

func TestWorkBuddyImpersonatesOfficialClient(t *testing.T) {
	incoming := http.Header{}
	incoming.Set("User-Agent", "DeepSeekHarness/1.0")
	incoming.Set("X-Session-Id", "dsh-session")
	incoming.Set("X-Agent-Intent", "coding")
	target := incoming.Clone()
	ApplyUpstreamClientHeaders(target, incoming, UpstreamClientIdentity{ChannelType: "workbuddy"})

	if got := target.Get("User-Agent"); got != workBuddyChatClientUA {
		t.Fatalf("User-Agent = %q, 必须伪装成官方 CLI", got)
	}
	if got := target.Get("Origin"); got != workBuddyBillingOrigin {
		t.Fatalf("Origin = %q", got)
	}
	if got := target.Get("Referer"); got != workBuddyBillingOrigin+"/" {
		t.Fatalf("Referer = %q", got)
	}
	if got := target.Get("X-Requested-With"); got != "XMLHttpRequest" {
		t.Fatalf("X-Requested-With = %q", got)
	}
	if got := target.Get("X-Product"); got != "SaaS" {
		t.Fatalf("X-Product = %q", got)
	}
	// conversation 身份三连必须齐全且形状正确。
	for _, name := range []string{"X-Conversation-ID", "X-Conversation-Request-ID", "X-Conversation-Message-ID"} {
		value := target.Get(name)
		if !workBuddyRequestIDPattern.MatchString(value) {
			t.Fatalf("%s = %q, 期望 32 位 UUIDv7", name, value)
		}
	}
	if target.Get("X-Conversation-ID") == target.Get("X-Conversation-Request-ID") {
		t.Fatal("conversation 三连每个值都应不同")
	}
	// 第三方会话标识不得泄漏到上游。
	for _, name := range []string{"X-Session-Id", "X-Agent-Intent"} {
		if got := target.Get(name); got != "" {
			t.Fatalf("%s = %q, 必须清除客户端自报的第三方标识", name, got)
		}
	}
	// X-Request-ID 与 message id 同值（官方客户端写法）。
	if target.Get("X-Request-ID") != target.Get("X-Conversation-Message-ID") {
		t.Fatal("X-Request-ID 应与 X-Conversation-Message-ID 同值")
	}
	// 官方身份未知时的 X-No-* 约定头。
	if got := target.Get("X-No-User-Id"); got != "1" {
		t.Fatalf("X-No-User-Id = %q", got)
	}
	// 不得改动客户端原始头集合。
	if incoming.Get("User-Agent") != "DeepSeekHarness/1.0" || incoming.Get("X-Session-Id") != "dsh-session" {
		t.Fatal("不得修改 incoming 头")
	}
}

func TestWorkBuddyOfficialHeadersFreshPerRequest(t *testing.T) {
	first := http.Header{}
	ApplyWorkBuddyOfficialHeaders(first)
	second := http.Header{}
	ApplyWorkBuddyOfficialHeaders(second)
	if first.Get("X-Conversation-ID") == second.Get("X-Conversation-ID") {
		t.Fatal("conversation id 必须每请求重新生成")
	}
}

func TestWorkBuddyOfficialHeadersKeepsProvidedUserId(t *testing.T) {
	target := http.Header{}
	target.Set("X-User-Id", "42")
	target.Set("X-Enterprise-Id", "ent-1")
	ApplyWorkBuddyOfficialHeaders(target)
	if target.Get("X-No-User-Id") != "" {
		t.Fatal("已知用户身份时不得再发 X-No-User-Id")
	}
	if target.Get("X-User-Id") != "42" || target.Get("X-Enterprise-Id") != "ent-1" {
		t.Fatal("已有的用户/企业头不得被改写")
	}
}