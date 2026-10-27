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
	// 签到窗口是计数口径：单个账号贡献「已签 1 / 共 1」，不再借用套餐档位字段
	// （用户要求弹窗顶部只显示真正的套餐档位，签到在下面单独一行）。
	if view.Level != "" {
		t.Fatalf("Level = %q, 签到窗口不应写入套餐档位", view.Level)
	}
	if len(view.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(view.Windows))
	}
	w := view.Windows[0]
	if w.Kind != workBuddyCheckinKind || w.UsedPercent != 100 {
		t.Fatalf("window kind/percent = %s/%v, want %s/100", w.Kind, w.UsedPercent, workBuddyCheckinKind)
	}
	if !w.Counts {
		t.Fatal("签到窗口必须是计数口径")
	}
	if w.DoneCount == nil || *w.DoneCount != 1 || w.AccountCount == nil || *w.AccountCount != 1 {
		t.Fatalf("计数 = %v/%v, want 1/1", w.DoneCount, w.AccountCount)
	}
	// 计数口径不展示额度三元组，避免被读成一笔余额。
	if w.Used != nil || w.Total != nil || w.Remaining != nil {
		t.Fatalf("计数窗口不应带 used/total/remaining：%+v", w)
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
	if view.Windows[0].DoneCount == nil || *view.Windows[0].DoneCount != 0 {
		t.Fatal("未签到时 DoneCount 必须是 0")
	}
}

// TestMergeWorkBuddyAccounts 守住「多账号合并」的口径（2026-10-27 用户要求）：
// 积分窗口按账号累加，签到窗口按「今天 N/M 个账号已签到」计数，而不是把各账号
// 的百分比取平均——后者会让「两个账号一个签了一个没签」显示成 50% 进度条。
func TestMergeWorkBuddyAccounts(t *testing.T) {
	checkedIn, err := parseWorkBuddyCheckin([]byte(`{"code":0,"data":{"today_checked_in":true}}`))
	if err != nil {
		t.Fatalf("解析已签账号失败：%v", err)
	}
	notCheckedIn, err := parseWorkBuddyCheckin([]byte(`{"code":0,"data":{"today_checked_in":false}}`))
	if err != nil {
		t.Fatalf("解析未签账号失败：%v", err)
	}
	creditsA, err := parseWorkBuddyCredits([]byte(`{"code":0,"data":{"Packages":[{"PackageCode":"A","CycleTotalCapacity":1000,"CycleUsedCapacity":100}]}}`))
	if err != nil {
		t.Fatalf("解析账号A积分失败：%v", err)
	}
	creditsB, err := parseWorkBuddyCredits([]byte(`{"code":0,"data":{"Packages":[{"PackageCode":"B","CycleTotalCapacity":2000,"CycleUsedCapacity":600}]}}`))
	if err != nil {
		t.Fatalf("解析账号B积分失败：%v", err)
	}

	merged := mergeQuotaViews([]QuotaView{
		mergeQuotaViews([]QuotaView{creditsA, checkedIn}),
		mergeQuotaViews([]QuotaView{creditsB, notCheckedIn}),
	})

	var checkin *QuotaWindow
	for index := range merged.Windows {
		if merged.Windows[index].Kind == workBuddyCheckinKind {
			checkin = &merged.Windows[index]
		}
	}
	if checkin == nil {
		t.Fatal("合并结果缺少签到窗口")
	}
	if checkin.DoneCount == nil || *checkin.DoneCount != 1 {
		t.Fatalf("已签到账号数 = %v, want 1", checkin.DoneCount)
	}
	if checkin.AccountCount == nil || *checkin.AccountCount != 2 {
		t.Fatalf("账号总数 = %v, want 2", checkin.AccountCount)
	}
	if checkin.UsedPercent != 50 {
		t.Fatalf("签到百分比 = %v, want 50（1/2）", checkin.UsedPercent)
	}
	// 积分窗口应按账号累加：总量 1000+2000，已用 100+600。
	for _, window := range merged.Windows {
		if window.Kind != workBuddyCreditsKind {
			continue
		}
		if window.Total == nil || *window.Total != 3000 {
			t.Fatalf("积分总量 = %v, want 3000", window.Total)
		}
		if window.Used == nil || *window.Used != 700 {
			t.Fatalf("积分已用 = %v, want 700", window.Used)
		}
	}
}
