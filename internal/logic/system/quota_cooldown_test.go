package system

import (
	"strings"
	"testing"
	"time"
)

func TestIsDailyQuotaExhaustion(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{"OpenRouter 日配额原文", "Rate limit exceeded: free-models-per-day (x50). Try again in 12h.", true},
		{"大小写混合", "Rate Limit Exceeded: FREE-MODELS-PER-DAY", true},
		{"普通 429 限流文案", "Rate limit exceeded: free-models-attempt-limit. Please try again soon.", false},
		{"提供商级限流", "Rate limit exceeded: poolside/laguna-xs-2.1:free", false},
		{"空消息", "", false},
		{"账号余额类", "credit balance is too low", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDailyQuotaExhaustion(tc.message); got != tc.want {
				t.Fatalf("isDailyQuotaExhaustion(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

func TestNextUTCMidnight(t *testing.T) {
	before := time.Now().UTC()
	next := nextUTCMidnight()
	after := time.Now().UTC()

	if !next.After(before) {
		t.Fatalf("nextUTCMidnight() = %v 必须晚于当前时刻 %v", next, before)
	}
	// 对齐 UTC 零点：加回当前时刻之前的当日部分应得到明天的零点。
	todayStart := time.Date(before.Year(), before.Month(), before.Day(), 0, 0, 0, 0, time.UTC)
	if want := todayStart.AddDate(0, 0, 1); !next.Equal(want) {
		t.Fatalf("nextUTCMidnight() = %v, want 明天 UTC 零点 %v", next, want)
	}
	if got := next.Location().String(); !strings.Contains(got, "UTC") {
		t.Fatalf("nextUTCMidnight() 时区 = %q, want UTC", got)
	}
	// 冷却跨度应不足 24 小时（当天任意时刻到期于次日零点）。
	if span := next.Sub(after); span > 24*time.Hour || span <= 0 {
		t.Fatalf("冷却跨度 %v 不在 (0, 24h] 内", span)
	}
}
