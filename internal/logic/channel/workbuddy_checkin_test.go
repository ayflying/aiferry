package channel

import (
	"testing"
	"time"
)

func TestParseWorkBuddyCheckinClaim(t *testing.T) {
	body := []byte(`{"code":0,"msg":"ok","data":{"credit":100,"streak_days":10,"is_streak_day":false}}`)
	claim, err := parseWorkBuddyCheckinClaim(body)
	if err != nil {
		t.Fatalf("parseWorkBuddyCheckinClaim error: %v", err)
	}
	if claim.Credit != 100 || claim.StreakDays != 10 || claim.IsStreakDay {
		t.Fatalf("unexpected claim: %+v", claim)
	}
}

func TestParseWorkBuddyCheckinClaimRejectsBusinessError(t *testing.T) {
	_, err := parseWorkBuddyCheckinClaim([]byte(`{"code":1,"msg":"already checked in"}`))
	if err == nil {
		t.Fatal("业务错误码必须返回错误")
	}
}

func TestWorkBuddyCheckedInToday(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		expect bool
	}{
		{"已签到", `{"code":0,"data":{"today_checked_in":true}}`, true},
		{"未签到", `{"code":0,"data":{"today_checked_in":false}}`, false},
		{"字段缺失按未签到", `{"code":0,"data":{}}`, false},
		{"非 JSON 按未签到", `not-json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workBuddyCheckedInToday([]byte(tc.body)); got != tc.expect {
				t.Fatalf("workBuddyCheckedInToday = %t, want %t", got, tc.expect)
			}
		})
	}
}

func TestWorkBuddyCheckinText(t *testing.T) {
	text := workBuddyCheckinText("#1", workBuddyCheckinClaim{Credit: 100, StreakDays: 10})
	if text == "" || text[len(text)-1] == ' ' {
		t.Fatalf("签到提示文案异常：%q", text)
	}
	// 整数不应渲染成 100.000000
	if got := workBuddyNumberText(100); got != "100" {
		t.Fatalf("workBuddyNumberText(100) = %q", got)
	}
	if got := workBuddyNumberText(0); got != "0" {
		t.Fatalf("workBuddyNumberText(0) = %q", got)
	}
}

// TestNextWorkBuddyCheckinAt 锁住「每天一次」的边界：未到 6 点排到今天 6:00，
// 正好 6 点与已过 6 点都排到明天，避免同日重复执行。
func TestNextWorkBuddyCheckinAt(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	cases := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"凌晨排到当天", time.Date(2026, 10, 28, 3, 20, 0, 0, zone), time.Date(2026, 10, 28, 6, 0, 0, 0, zone)},
		{"正好整点排到明天", time.Date(2026, 10, 28, 6, 0, 0, 0, zone), time.Date(2026, 10, 29, 6, 0, 0, 0, zone)},
		{"白天排到明天", time.Date(2026, 10, 28, 11, 9, 0, 0, zone), time.Date(2026, 10, 29, 6, 0, 0, 0, zone)},
	}
	for _, item := range cases {
		if got := nextWorkBuddyCheckinAt(item.from); !got.Equal(item.want) {
			t.Fatalf("%s：nextWorkBuddyCheckinAt(%s) = %s, want %s", item.name, item.from, got, item.want)
		}
	}
}
