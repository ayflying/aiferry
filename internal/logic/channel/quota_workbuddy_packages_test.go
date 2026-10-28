package channel

import (
	"math"
	"testing"
)

// TestCollectWorkBuddyPackagesPascalCase 锁住历史桌面端网页接口的形状：
// Packages 数组 + PascalCase 容量字段。改解析实现时这条必须继续通过。
func TestCollectWorkBuddyPackagesPascalCase(t *testing.T) {
	body := []byte(`{"code":0,"msg":"ok","data":{"Packages":[
		{"PackageCode":"monthly","CycleTotalCapacity":3000,"CycleRemainCapacity":1500,"CycleUsedCapacity":1500}
	],"SubscriptionPackageCode":"pro","IsPaidUser":true}}`)

	packages := collectWorkBuddyPackages(body)
	if len(packages) != 1 {
		t.Fatalf("packages = %d, want 1", len(packages))
	}
	got := packages[0]
	if got.PackageCode != "monthly" || got.Total != 3000 || got.Remain != 1500 || got.Used != 1500 {
		t.Fatalf("package = %+v", got)
	}
}

// TestCollectWorkBuddyPackagesSnakeCase 覆盖没有 Packages 时的历史兼容：
// 小写下划线命名与不同嵌套层级，不代表 /v2 接口已验证可用。
func TestCollectWorkBuddyPackagesSnakeCase(t *testing.T) {
	body := []byte(`{"code":0,"data":{"user_resource":{
		"package_name":"free","total_capacity":"200","remain_capacity":"80","used_capacity":"120"}}}`)

	packages := collectWorkBuddyPackages(body)
	if len(packages) != 1 {
		t.Fatalf("packages = %d, want 1", len(packages))
	}
	got := packages[0]
	if got.PackageCode != "free" || got.Total != 200 || got.Remain != 80 || got.Used != 120 {
		t.Fatalf("package = %+v", got)
	}
}

// TestCollectWorkBuddyPackagesDedupes 递归扫描会从不同层级拿到同一份对象，
// 重复累加会把积分翻倍，比查不到更有害，所以必须去重。
func TestCollectWorkBuddyPackagesDedupes(t *testing.T) {
	body := []byte(`{"data":{"summary":{"CycleTotalCapacity":100,"CycleRemainCapacity":40},
		"legacy_packages":[{"PackageCode":"a","CycleTotalCapacity":100,"CycleRemainCapacity":40}]}}`)

	packages := collectWorkBuddyPackages(body)
	// 缺少 Packages 时仍递归兼容；容量相同但套餐名不同的对象保留两条。
	// 完全相同（含套餐名）的重复必须只留一条。
	same := []byte(`{"data":{"Packages":[
		{"PackageCode":"a","CycleTotalCapacity":100,"CycleRemainCapacity":40},
		{"PackageCode":"a","CycleTotalCapacity":100,"CycleRemainCapacity":40}]}}`)
	if got := len(collectWorkBuddyPackages(same)); got != 1 {
		t.Fatalf("same packages = %d, want 1", got)
	}
	if got := len(packages); got != 2 {
		t.Fatalf("packages = %d, want 2", got)
	}
}

// TestParseWorkBuddyCreditsPackagesExcludeCreditGroups 锁住官网同时返回两份额度视图时不双算。
func TestParseWorkBuddyCreditsPackagesExcludeCreditGroups(t *testing.T) {
	body := []byte(`{"code":0,"data":{"Packages":[
		{"PackageCode":"monthly","CycleTotalCapacity":3000,"CycleRemainCapacity":2630,"CycleUsedCapacity":370},
		{"PackageCode":"bonus","CycleTotalCapacity":500,"CycleRemainCapacity":485.73000005,"CycleUsedCapacity":14.26999995},
		{"PackageCode":"gift","CycleTotalCapacity":100,"CycleRemainCapacity":100,"CycleUsedCapacity":0}
	],"CreditGroups":[
		{"TotalCredit":3600,"RemainCredit":3215.73000005,"UsedCredit":384.26999995}
	]}}`)

	packages := collectWorkBuddyPackages(body)
	if len(packages) != 3 {
		t.Fatalf("packages = %d, want 3", len(packages))
	}
	view, err := parseWorkBuddyCredits(body)
	if err != nil {
		t.Fatalf("parse err = %v", err)
	}
	if len(view.Windows) != 3 {
		t.Fatalf("windows = %d, want 3", len(view.Windows))
	}
	var remaining float64
	for _, window := range view.Windows {
		if window.Remaining == nil {
			t.Fatal("remaining is nil")
		}
		remaining += *window.Remaining
	}
	if math.Abs(remaining-3215.73000005) > 1e-9 {
		t.Fatalf("remaining = %.11f, want 3215.73000005", remaining)
	}
}

func TestCollectWorkBuddyPackagesEmptyPackagesExcludeMirrors(t *testing.T) {
	body := []byte(`{"code":0,"data":{"Packages":[],"CreditGroups":[
		{"TotalCredit":3600,"RemainCredit":3215.73000005,"UsedCredit":384.26999995}
	]}}`)
	if packages := collectWorkBuddyPackages(body); len(packages) != 0 {
		t.Fatalf("packages = %+v, want none for authoritative empty Packages", packages)
	}
	if _, err := parseWorkBuddyCredits(body); err == nil {
		t.Fatal("empty Packages must not produce quota windows from mirrors")
	}
}

func TestCollectWorkBuddyPackagesWithoutPackagesUsesCreditGroups(t *testing.T) {
	body := []byte(`{"data":{"CreditGroups":[
		{"TotalCredit":3600,"RemainCredit":3215.73000005,"UsedCredit":384.26999995}
	]}}`)
	packages := collectWorkBuddyPackages(body)
	if len(packages) != 1 || packages[0].Remain != 3215.73000005 {
		t.Fatalf("packages = %+v, want historical CreditGroups fallback", packages)
	}
}

// TestCollectWorkBuddyPackagesIgnoresUnrelated 裸露的 total/used 字段不算额度，
// 否则计费明细之类的对象会被误当套餐，余额直接算错。
func TestCollectWorkBuddyPackagesIgnoresUnrelated(t *testing.T) {
	body := []byte(`{"data":{"billing":{"total":100,"used":20,"currency":"USD"}}}`)
	if got := collectWorkBuddyPackages(body); len(got) != 0 {
		t.Fatalf("packages = %v, want none", got)
	}
}

// TestParseWorkBuddyCreditsSnakeCase 验证新形状能一路走到额度窗口，
// 且总量缺失时用 已用 + 剩余 还原。
func TestParseWorkBuddyCreditsSnakeCase(t *testing.T) {
	body := []byte(`{"code":0,"data":{"total_capacity":0,"remain_capacity":30,"used_capacity":70}}`)

	view, err := parseWorkBuddyCredits(body)
	if err != nil {
		t.Fatalf("parse err = %v", err)
	}
	if len(view.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(view.Windows))
	}
	window := view.Windows[0]
	if window.Total == nil || *window.Total != 100 {
		t.Fatalf("total = %v, want 100", window.Total)
	}
	if window.Remaining == nil || *window.Remaining != 30 {
		t.Fatalf("remaining = %v, want 30", window.Remaining)
	}
	if window.Used == nil || *window.Used != 70 {
		t.Fatalf("used = %v, want 70", window.Used)
	}
}
