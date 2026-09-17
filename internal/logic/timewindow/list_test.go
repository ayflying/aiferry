package timewindow

import (
	"testing"
	"time"
)

func mustParseList(t *testing.T, raw string) []Window {
	t.Helper()
	windows, err := ParseList(raw)
	if err != nil {
		t.Fatalf("ParseList(%s) error = %v", raw, err)
	}
	return windows
}

func TestParseListAcceptsArrayLegacyObjectAndEmpty(t *testing.T) {
	windows := mustParseList(t, `[
		{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","18:00"]]},
		{"tz":"Asia/Shanghai","weekdays":[6,7],"ranges":[["00:00","00:00"]]}
	]`)
	if len(windows) != 2 {
		t.Fatalf("数组形态应解析出 2 个窗口，got %d", len(windows))
	}
	if len(windows[0].Weekdays) != 5 || len(windows[1].Ranges) != 1 {
		t.Fatalf("窗口内容解析异常：%+v", windows)
	}

	legacy := mustParseList(t, `{"tz":"Asia/Shanghai","weekdays":[1],"ranges":[["09:00","12:00"]]}`)
	if len(legacy) != 1 || legacy[0].Weekdays[0] != 1 {
		t.Fatalf("旧版单对象应包装成单元素列表：%+v", legacy)
	}

	for _, raw := range []string{"", "   ", "null", "[]"} {
		if windows := mustParseList(t, raw); len(windows) != 0 {
			t.Fatalf("ParseList(%q) 应为空列表，got %+v", raw, windows)
		}
	}
}

func TestParseListRejectsInvalidContent(t *testing.T) {
	cases := map[string]string{
		"整个不是 JSON":  `not-json`,
		"数组里混入字符串":   `["09:00"]`,
		"窗口星期越界":     `[{"weekdays":[0],"ranges":[["09:00","12:00"]]}]`,
		"窗口时段不是合法时间": `[{"ranges":[["9:00","12:00"]]}]`,
	}
	for name, raw := range cases {
		if _, err := ParseList(raw); err == nil {
			t.Fatalf("%s：ParseList(%s) 期望报错却通过了", name, raw)
		}
	}
}

func TestNormalizeWindowsDropsUnconfiguredAndSerializesArray(t *testing.T) {
	// ranges 只去重、保持输入顺序（用户按顺序添加时段，不该被悄悄重排）。
	normalized, err := NormalizeWindows([]Window{
		{TZ: "Asia/Shanghai", Weekdays: []int{5, 1, 1}, Ranges: [][]string{{"14:00", "18:00"}, {"09:00", "12:00"}, {"09:00", "12:00"}}},
		{TZ: "Asia/Shanghai"},
		{Weekdays: []int{1, 2, 3, 4, 5, 6, 7}},
	})
	if err != nil {
		t.Fatalf("NormalizeWindows error = %v", err)
	}
	want := `[{"tz":"Asia/Shanghai","weekdays":[1,5],"ranges":[["14:00","18:00"],["09:00","12:00"]]}]`
	if normalized != want {
		t.Fatalf("NormalizeWindows = %s, want %s", normalized, want)
	}

	// 全部等价于「不限制」时返回空串，调用方据此清空列。
	empty, err := NormalizeWindows([]Window{{TZ: "Asia/Shanghai"}, {Weekdays: []int{1, 2, 3, 4, 5, 6, 7}}})
	if err != nil {
		t.Fatalf("NormalizeWindows error = %v", err)
	}
	if empty != "" {
		t.Fatalf("全不限制的列表应归一成空串，got %q", empty)
	}

	if _, err := NormalizeWindows([]Window{{Weekdays: []int{8}}}); err == nil {
		t.Fatal("星期越界的窗口应报错")
	}
}

// TestAnyContainsMatchesAnyRule 固定列表语义：任一窗口命中即关闭（OR）。
func TestAnyContainsMatchesAnyRule(t *testing.T) {
	windows := mustParseList(t, `[
		{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","18:00"]]},
		{"tz":"Asia/Shanghai","weekdays":[6,7],"ranges":[["00:00","00:00"]]}
	]`)
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"周一工作时段命中第一条", shanghaiTime(t, 2026, time.September, 14, 10, 0), true},
		{"周一起点前不命中", shanghaiTime(t, 2026, time.September, 14, 8, 59), false},
		{"周一终点后不命中", shanghaiTime(t, 2026, time.September, 14, 18, 0), false},
		{"周六凌晨命中第二条全天", shanghaiTime(t, 2026, time.September, 19, 3, 0), true},
		{"周日上午命中第二条全天", shanghaiTime(t, 2026, time.September, 20, 11, 0), true},
	}
	for _, item := range cases {
		if got := AnyContains(windows, item.at); got != item.want {
			t.Fatalf("%s：AnyContains = %v, want %v", item.name, got, item.want)
		}
	}
	if AnyContains(nil, shanghaiTime(t, 2026, time.September, 14, 10, 0)) {
		t.Fatal("空列表不应命中任何时刻")
	}
}
