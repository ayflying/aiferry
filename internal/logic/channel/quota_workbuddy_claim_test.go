package channel

import (
	"net/url"
	"testing"
)

// TestWorkBuddyClaimEndpoint 守住签到领取地址的拼装。
//
// 2026-10-27 生产日志里出现过同一账号既「今天已签到，跳过」又「领取失败 HTTP 404」：
// 旧实现用 strings.TrimSuffix(checkinEndpoint, workBuddyCheckinPath) 推导领取地址，
// 但 checkinEndpoint 是 resolveHostURL 拼好的完整 URL，末尾带 /v2 前缀，
// 而 workBuddyCheckinPath 不含 /v2，TrimSuffix 匹配不上，于是「领取」实际打到了
// 状态查询地址，必然 404。这里直接校验拼出来的路径，防止再退回那种字符串推导。
func TestWorkBuddyClaimEndpoint(t *testing.T) {
	baseURL := "https://copilot.tencent.com"
	endpoint, err := resolveHostURL(baseURL, workBuddyDailyCheckinPath)
	if err != nil {
		t.Fatalf("拼装领取地址失败：%v", err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("领取地址不是合法 URL：%v", err)
	}
	if parsed.Path != "/v2/billing/meter/daily-checkin" {
		t.Fatalf("领取路径 = %q，期望 /v2/billing/meter/daily-checkin", parsed.Path)
	}
	if parsed.Host != "copilot.tencent.com" {
		t.Fatalf("领取主机 = %q，期望 copilot.tencent.com", parsed.Host)
	}
	// 状态查询与领取必须是两个不同地址，否则又会打错端点。
	statusEndpoint, err := resolveHostURL(baseURL, "/v2"+workBuddyCheckinPath)
	if err != nil {
		t.Fatalf("拼装状态地址失败：%v", err)
	}
	if endpoint == statusEndpoint {
		t.Fatal("领取地址不能等于状态查询地址")
	}
}
