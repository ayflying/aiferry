package channel

import (
	"strings"
	"testing"
)

func TestParseWorkBuddyCredits(t *testing.T) {
	// 桌面端 get-user-resource-summary 实测结构：Packages 为 PascalCase 字段名。
	body := []byte(`{"code":0,"msg":"ok","data":{` +
		`"Packages":[{"PackageCode":"pro_year","CycleTotalCapacity":3000,` +
		`"CycleRemainCapacity":1500,"CycleUsedCapacity":1500}],` +
		`"SubscriptionPackageCode":"pro_year","IsPaidUser":true}}`)
	view, err := parseWorkBuddyCredits(body)
	if err != nil {
		t.Fatalf("parseWorkBuddyCredits error: %v", err)
	}
	if view.Mode != "workbuddy_credits" {
		t.Fatalf("Mode = %q, want workbuddy_credits", view.Mode)
	}
	if view.Level != "pro_year" {
		t.Fatalf("Level = %q, want pro_year", view.Level)
	}
	if len(view.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(view.Windows))
	}
	w := view.Windows[0]
	if w.Kind != workBuddyCreditsKind || w.Label != "pro_year" {
		t.Fatalf("window kind/label = %s/%s, want %s/pro_year", w.Kind, w.Label, workBuddyCreditsKind)
	}
	if w.Used == nil || *w.Used != 1500 || w.Total == nil || *w.Total != 3000 || w.Remaining == nil || *w.Remaining != 1500 {
		t.Fatalf("used/total/remaining = %v/%v/%v, want 1500/3000/1500", w.Used, w.Total, w.Remaining)
	}
	if w.UsedPercent != 50 {
		t.Fatalf("UsedPercent = %v, want 50", w.UsedPercent)
	}
}

func TestParseWorkBuddyCreditsDerivesUsedFromTotalMinusRemain(t *testing.T) {
	// 上游未返回 CycleUsedCapacity 时用「总 − 剩余」补齐已用。
	body := []byte(`{"code":0,"data":{"Packages":[{"PackageCode":"free",` +
		`"CycleTotalCapacity":100,"CycleRemainCapacity":40}],"IsPaidUser":false}}`)
	view, err := parseWorkBuddyCredits(body)
	if err != nil {
		t.Fatalf("parseWorkBuddyCredits error: %v", err)
	}
	if view.Level != "免费用户" {
		t.Fatalf("Level = %q, want 免费用户", view.Level)
	}
	w := view.Windows[0]
	if w.Used == nil || *w.Used != 60 {
		t.Fatalf("Used = %v, want 60", w.Used)
	}
	if w.UsedPercent != 60 {
		t.Fatalf("UsedPercent = %v, want 60", w.UsedPercent)
	}
}

func TestParseWorkBuddyCreditsRejectsEmptyPackages(t *testing.T) {
	_, err := parseWorkBuddyCredits([]byte(`{"code":0,"data":{"Packages":[]}}`))
	if err == nil || !strings.Contains(err.Error(), "未返回有效的积分额度窗口") {
		t.Fatalf("err = %v, want 未返回有效的积分额度窗口", err)
	}
}

func TestParseWorkBuddyCreditsError(t *testing.T) {
	_, err := parseWorkBuddyCredits([]byte(`{"code":1,"msg":"invalid token"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("err = %v, want invalid token message", err)
	}
}

func TestParseWorkBuddyCheckin(t *testing.T) {
	body := []byte(`{"code":0,"msg":"ok","data":{"active":true,"today_checked_in":true,` +
		`"streak_days":9,"is_streak_day":false,"next_streak_day":0,"today_credit":100,` +
		`"streak_bonus_days":0,"streak_bonus_credit":0}}`)
	view, err := parseWorkBuddyCheckin(body)
	if err != nil {
		t.Fatalf("parseWorkBuddyCheckin error: %v", err)
	}
	if view.Level != "签到" {
		t.Fatalf("Level = %q, want 签到", view.Level)
	}
	if len(view.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(view.Windows))
	}
	w := view.Windows[0]
	if w.Kind != workBuddyCheckinKind || w.UsedPercent != 100 {
		t.Fatalf("window kind/percent = %s/%v, want %s/100", w.Kind, w.UsedPercent, workBuddyCheckinKind)
	}
	if w.Used == nil || *w.Used != 9 {
		t.Fatalf("Used (streak_days) = %v, want 9", w.Used)
	}
	if w.Total == nil || *w.Total != 100 {
		t.Fatalf("Total (today_credit) = %v, want 100", w.Total)
	}
}

func TestParseWorkBuddyCheckinNotCheckedInToday(t *testing.T) {
	body := []byte(`{"code":0,"data":{"active":true,"today_checked_in":false,` +
		`"streak_days":3,"today_credit":100}}`)
	view, err := parseWorkBuddyCheckin(body)
	if err != nil {
		t.Fatalf("parseWorkBuddyCheckin error: %v", err)
	}
	if view.Windows[0].UsedPercent != 0 {
		t.Fatalf("UsedPercent = %v, want 0 when not checked in", view.Windows[0].UsedPercent)
	}
}