package channel

import (
	"net/http"
	"testing"
)

// TestWorkBuddyCreditsVariantsOrder 锁住候选形态的顺序：先同源浏览器形态，
// 再回退到 0.6.27 之后的 chat 身份形态。顺序错了会把已知失败的形态放在前面，
// 每次查询都多打一次必然 401 的上游请求。
func TestWorkBuddyCreditsVariantsOrder(t *testing.T) {
	if len(workBuddyCreditsRequestVariants) != 2 {
		t.Fatalf("候选形态数量 = %d，期望 2", len(workBuddyCreditsRequestVariants))
	}
	first := workBuddyCreditsRequestVariants[0]
	if first.Name != "legacy-api" {
		t.Fatalf("首个形态 = %q，期望 legacy-api", first.Name)
	}
	if !first.SkipIdentity || first.Apply != nil {
		t.Fatal("legacy-api 必须跳过 chat 身份头且不追加浏览器头")
	}
	second := workBuddyCreditsRequestVariants[1]
	if second.Name != "browser-origin" || !second.SkipIdentity || second.Apply == nil {
		t.Fatalf("回退形态 = %q skip=%v，期望 browser-origin 且跳过 chat 身份头", second.Name, second.SkipIdentity)
	}
}

// TestApplyWorkBuddyCreditsBrowserHeaders 验证浏览器形态与请求主机同源，
// 且不残留 chat 契约专属的身份头——跨站 Origin 正是 2026-10-27 之后
// 积分接口被会话门挡成 401 的原因。
func TestApplyWorkBuddyCreditsBrowserHeaders(t *testing.T) {
	target := http.Header{}
	target.Set("X-Conversation-ID", "leftover")
	target.Set("X-Request-ID", "leftover")
	target.Set("X-Product", "SaaS")
	target.Set("X-No-User-Id", "1")
	target.Set("Authorization", "Bearer token")

	applyWorkBuddyCreditsBrowserHeaders(target)

	if got := target.Get("Origin"); got != workBuddyCreditsBrowserOrigin {
		t.Fatalf("Origin = %q，期望 %q", got, workBuddyCreditsBrowserOrigin)
	}
	if got := target.Get("Referer"); got != workBuddyCreditsBrowserOrigin+"/" {
		t.Fatalf("Referer = %q，期望 %q", got, workBuddyCreditsBrowserOrigin+"/")
	}
	// Origin 必须与积分接口同源，否则会话门按跨站请求拒绝。
	if workBuddyCreditsBrowserOrigin == workBuddyBillingOrigin {
		t.Fatal("积分站点与 chat 控制面域名相同，说明常量配错了")
	}
	for _, name := range workBuddyChatIdentityHeaders {
		if value := target.Get(name); value != "" {
			t.Fatalf("浏览器形态残留 chat 身份头 %s=%q", name, value)
		}
	}
	if got := target.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("鉴权头被破坏：%q", got)
	}
}
