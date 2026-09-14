package timewindow

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, raw string) Window {
	t.Helper()
	window, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%s) error = %v", raw, err)
	}
	return window
}

func TestParseEmptyMeansUnconfigured(t *testing.T) {
	for _, raw := range []string{"", "   ", "null"} {
		window := mustParse(t, raw)
		if !window.IsZero() {
			t.Fatalf("Parse(%q) = %+v, want zero window", raw, window)
		}
		if window.Contains(time.Now()) {
			t.Fatalf("Parse(%q) 未配置时不应命中任何时刻", raw)
		}
	}
}

func TestParseRejectsInvalidContent(t *testing.T) {
	cases := map[string]string{
		"非法 JSON":  `{"tz":`,
		"未知时区":     `{"tz":"Mars/Olympus","ranges":[["09:00","12:00"]]}`,
		"星期越界":     `{"weekdays":[0],"ranges":[["09:00","12:00"]]}`,
		"星期超过 7":   `{"weekdays":[8],"ranges":[["09:00","12:00"]]}`,
		"时段不是两个元素": `{"ranges":[["09:00"]]}`,
		"时段不是合法时间": `{"ranges":[["9:00","12:00"]]}`,
		"时段小时越界":   `{"ranges":[["24:00","12:00"]]}`,
		"时段分钟越界":   `{"ranges":[["09:60","12:00"]]}`,
	}
	for name, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("%s：Parse(%s) 期望报错却通过了", name, raw)
		}
	}
}

func TestNormalizeSortsAndDeduplicates(t *testing.T) {
	normalized, err := Normalize(`{"tz":" Asia/Shanghai ","weekdays":[5,1,1,3],"ranges":[["09:00","12:00"],["09:00","12:00"],["14:00","18:00"]]}`)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	want := `{"tz":"Asia/Shanghai","weekdays":[1,3,5],"ranges":[["09:00","12:00"],["14:00","18:00"]]}`
	if normalized != want {
		t.Fatalf("Normalize = %s, want %s", normalized, want)
	}
}

func TestNormalizeClearsUnconfiguredWindow(t *testing.T) {
	// 只写时区或全选七天都等价于「不限制」，必须归一成空串，
	// 否则「清空关闭时段」会退化成一条只带时区、实际不生效的脏记录。
	cases := []string{
		"",
		"null",
		"{}",
		`{"weekdays":[],"ranges":[]}`,
		`{"tz":"Asia/Shanghai"}`,
		`{"weekdays":[1,2,3,4,5,6,7]}`,
	}
	for _, raw := range cases {
		normalized, err := Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize(%s) error = %v", raw, err)
		}
		if normalized != "" {
			t.Fatalf("Normalize(%s) = %q, want empty", raw, normalized)
		}
	}
}

func TestNormalizeKeepsMeaningfulWindow(t *testing.T) {
	normalized, err := Normalize(`{"weekdays":[1,2,3,4,5]}`)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if normalized != `{"weekdays":[1,2,3,4,5]}` {
		t.Fatalf("Normalize = %s, want 保留星期限制", normalized)
	}
	allDay, err := Normalize(`{"ranges":[["00:00","00:00"]]}`)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if allDay != `{"ranges":[["00:00","00:00"]]}` {
		t.Fatalf("Normalize = %s, want 保留全天时段", allDay)
	}
	// 去重后才判断「全选七天」：七个重复的周一只是「只关周一」，属于真实限制。
	repeated, err := Normalize(`{"weekdays":[1,1,1,1,1,1,1]}`)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if repeated != `{"weekdays":[1]}` {
		t.Fatalf("Normalize = %s, want 去重后保留周一", repeated)
	}
}

// 2026-09-14 是周一，2026-09-19 是周六，用它们验证星期维度。
func shanghaiTime(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Shanghai) error = %v", err)
	}
	return time.Date(year, month, day, hour, minute, 0, 0, location)
}

func TestContainsAppliesWeekdaysAndRanges(t *testing.T) {
	window := mustParse(t, `{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","12:00"],["14:00","18:00"]]}`)
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"工作日高峰上午", shanghaiTime(t, 2026, time.September, 14, 10, 0), true},
		{"工作日高峰下午", shanghaiTime(t, 2026, time.September, 14, 14, 30), true},
		{"时段起点含", shanghaiTime(t, 2026, time.September, 14, 9, 0), true},
		{"时段终点不含", shanghaiTime(t, 2026, time.September, 14, 12, 0), false},
		{"两段之间的空档", shanghaiTime(t, 2026, time.September, 14, 13, 0), false},
		{"工作日夜间", shanghaiTime(t, 2026, time.September, 14, 20, 0), false},
		{"周六不在星期列表", shanghaiTime(t, 2026, time.September, 19, 10, 0), false},
	}
	for _, item := range cases {
		if got := window.Contains(item.at); got != item.want {
			t.Fatalf("%s：Contains = %v, want %v", item.name, got, item.want)
		}
	}
}

func TestContainsSupportsCrossMidnightAndAllDay(t *testing.T) {
	overnight := mustParse(t, `{"tz":"Asia/Shanghai","ranges":[["22:00","06:00"]]}`)
	if !overnight.Contains(shanghaiTime(t, 2026, time.September, 14, 23, 30)) {
		t.Fatal("跨零点时段应命中 23:30")
	}
	if !overnight.Contains(shanghaiTime(t, 2026, time.September, 15, 5, 0)) {
		t.Fatal("跨零点时段应命中次日 05:00")
	}
	if overnight.Contains(shanghaiTime(t, 2026, time.September, 15, 12, 0)) {
		t.Fatal("跨零点时段不应命中 12:00")
	}

	allDay := mustParse(t, `{"tz":"Asia/Shanghai","weekdays":[6,7],"ranges":[["00:00","00:00"]]}`)
	if !allDay.Contains(shanghaiTime(t, 2026, time.September, 19, 3, 0)) {
		t.Fatal("起止相同时段应视为全天（周六）")
	}
	if allDay.Contains(shanghaiTime(t, 2026, time.September, 14, 3, 0)) {
		t.Fatal("全天时段仍应受星期限制（周一不在列表）")
	}
}

func TestContainsUsesDefaultTimezoneWhenMissing(t *testing.T) {
	window := mustParse(t, `{"ranges":[["17:00","19:00"]]}`)
	// 同一时刻：UTC 10:00 即北京时间 18:00，落在默认时区的时间窗内。
	at := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	if !window.Contains(at) {
		t.Fatal("未指定 tz 时应按北京时间判定")
	}
	explicitUTC := mustParse(t, `{"tz":"UTC","ranges":[["17:00","19:00"]]}`)
	if explicitUTC.Contains(at) {
		t.Fatal("显式指定 UTC 时不应命中")
	}
}

func TestContainsIgnoresWeekdaysWhenEmpty(t *testing.T) {
	window := mustParse(t, `{"tz":"Asia/Shanghai","ranges":[["09:00","12:00"]]}`)
	if !window.Contains(shanghaiTime(t, 2026, time.September, 19, 10, 0)) {
		t.Fatal("星期为空表示每天都生效")
	}
}

// TestAllowsTreatsUndeclaredDimensionsAsUnrestricted 固定 Contains 与 Allows 的分界：
// Contains 是「落在关闭窗口内」，没有时段就不构成窗口；Allows 是「条件是否适用于该时刻」，
// 未声明的维度一律视为不限，因此不限制的时间窗恒为真。
func TestAllowsTreatsUndeclaredDimensionsAsUnrestricted(t *testing.T) {
	for _, raw := range []string{"", "null", `{}`, `{"tz":"Asia/Shanghai"}`, `{"weekdays":[1,2,3,4,5,6,7]}`} {
		window := mustParse(t, raw)
		if !window.Allows(shanghaiTime(t, 2026, time.September, 14, 3, 0)) {
			t.Fatalf("Allows(%s) 不限制的时间窗应恒为真", raw)
		}
	}
	// 只声明星期：这些星期全天适用，其余星期不适用。同一时间窗的 Contains 恒为假。
	window := mustParse(t, `{"weekdays":[6,7]}`)
	if !window.Allows(shanghaiTime(t, 2026, time.September, 19, 3, 0)) {
		t.Fatal("只声明星期六日时，周六任意时刻都应允许")
	}
	if window.Allows(shanghaiTime(t, 2026, time.September, 14, 3, 0)) {
		t.Fatal("只声明星期六日时，周一不应允许")
	}
	if window.Contains(shanghaiTime(t, 2026, time.September, 19, 3, 0)) {
		t.Fatal("没有时段就不构成关闭窗口，Contains 应为假")
	}
}

func TestAllowsAppliesWeekdaysAndRanges(t *testing.T) {
	window := mustParse(t, `{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","12:00"],["14:00","18:00"]]}`)
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"工作日高峰内", shanghaiTime(t, 2026, time.September, 14, 10, 0), true},
		{"时段终点不含", shanghaiTime(t, 2026, time.September, 14, 12, 0), false},
		{"晚间非高峰", shanghaiTime(t, 2026, time.September, 14, 18, 55), false},
		{"周六不属工作日", shanghaiTime(t, 2026, time.September, 19, 10, 0), false},
	}
	for _, item := range cases {
		if got := window.Allows(item.at); got != item.want {
			t.Fatalf("%s：Allows = %v, want %v", item.name, got, item.want)
		}
	}
	// 与 Contains 在「有时段」这一支上必须一致。
	if window.Contains(shanghaiTime(t, 2026, time.September, 14, 10, 0)) != window.Allows(shanghaiTime(t, 2026, time.September, 14, 10, 0)) {
		t.Fatal("有具体时段时 Contains 与 Allows 判定应一致")
	}
}
