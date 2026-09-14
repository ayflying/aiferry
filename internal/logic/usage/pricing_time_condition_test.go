package usage

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// 复刻线上的 deepseek-v4.1-flash 两条规则条件：高价区带时段，空闲时段不限制时段。
// 计费按 priority 降序逐条试匹配，高价区没命中才轮到空闲时段兜底。
const (
	peakTimeCondition    = `{"time":{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","12:00"],["14:00","18:00"]]}}`
	offPeakTimeCondition = `{"time":{"tz":"Asia/Shanghai"}}`
)

func shanghaiLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Shanghai) error = %v", err)
	}
	return location
}

// TestRuleMatchesHonorsPeakTimeWindow 覆盖上线前的真实故障：
// conditions.time 不被解析时，带时段的高价区规则会在全天命中，
// 让非高峰时段也按高价计费。
func TestRuleMatchesHonorsPeakTimeWindow(t *testing.T) {
	location := shanghaiLocation(t)
	// 2026-09-14 是周一，09-19 / 09-20 是周六 / 周日。
	cases := []struct {
		name    string
		at      time.Time
		peak    bool
		offPeak bool
	}{
		{name: "工作日高峰内 10:00", at: time.Date(2026, 9, 14, 10, 0, 0, 0, location), peak: true, offPeak: true},
		{name: "时段起点 14:00 含边界", at: time.Date(2026, 9, 14, 14, 0, 0, 0, location), peak: true, offPeak: true},
		{name: "时段终点前 11:59", at: time.Date(2026, 9, 14, 11, 59, 0, 0, location), peak: true, offPeak: true},
		{name: "时段终点 12:00 不含边界", at: time.Date(2026, 9, 14, 12, 0, 0, 0, location), peak: false, offPeak: true},
		{name: "两段之间的空档 12:30", at: time.Date(2026, 9, 14, 12, 30, 0, 0, location), peak: false, offPeak: true},
		{name: "时段终点 18:00 不含边界", at: time.Date(2026, 9, 14, 18, 0, 0, 0, location), peak: false, offPeak: true},
		{name: "晚间非高峰 18:55", at: time.Date(2026, 9, 14, 18, 55, 0, 0, location), peak: false, offPeak: true},
		{name: "早间非高峰 08:59", at: time.Date(2026, 9, 14, 8, 59, 0, 0, location), peak: false, offPeak: true},
		{name: "周六不属工作日", at: time.Date(2026, 9, 19, 10, 0, 0, 0, location), peak: false, offPeak: true},
		{name: "周日不属工作日", at: time.Date(2026, 9, 20, 10, 0, 0, 0, location), peak: false, offPeak: true},
		// 容器本地时区通常是 UTC：同一时刻用 UTC 表达仍须按 Asia/Shanghai 判定，
		// 否则时段会整体偏移 8 小时。
		{name: "以 UTC 表达的北京 10:00", at: time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC), peak: true, offPeak: true},
	}
	for _, item := range cases {
		if got := RuleMatches(peakTimeCondition, "", TokenUsage{}, item.at); got != item.peak {
			t.Errorf("%s：高价区规则命中 = %t，期望 %t", item.name, got, item.peak)
		}
		if got := RuleMatches(offPeakTimeCondition, "", TokenUsage{}, item.at); got != item.offPeak {
			t.Errorf("%s：空闲时段规则命中 = %t，期望 %t", item.name, got, item.offPeak)
		}
	}
}

// TestRuleMatchesTimeWindowEdges 覆盖未声明维度、跨零点、全天与非法写法。
func TestRuleMatchesTimeWindowEdges(t *testing.T) {
	location := shanghaiLocation(t)
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 9, 14, hour, minute, 0, 0, location)
	}
	saturday := time.Date(2026, 9, 19, 3, 0, 0, 0, location)
	cases := []struct {
		name       string
		conditions string
		at         time.Time
		want       bool
	}{
		{name: "无 time 块表示不限", conditions: `{}`, at: at(3, 0), want: true},
		{name: "time 为 null 表示不限", conditions: `{"time":null}`, at: at(3, 0), want: true},
		{name: "只写时区表示不限", conditions: offPeakTimeCondition, at: at(3, 0), want: true},
		{name: "七天全选等价于不限", conditions: `{"time":{"weekdays":[1,2,3,4,5,6,7]}}`, at: at(3, 0), want: true},
		{name: "只写星期则为这些星期全天", conditions: `{"time":{"weekdays":[6,7]}}`, at: saturday, want: true},
		{name: "只写星期不覆盖工作日", conditions: `{"time":{"weekdays":[6,7]}}`, at: at(3, 0), want: false},
		{name: "起止相同表示全天", conditions: `{"time":{"ranges":[["00:00","00:00"]]}}`, at: at(3, 0), want: true},
		{name: "跨零点区间命中当日深夜", conditions: `{"time":{"ranges":[["22:00","06:00"]]}}`, at: at(23, 30), want: true},
		{name: "跨零点区间命中次日凌晨", conditions: `{"time":{"ranges":[["22:00","06:00"]]}}`, at: at(5, 59), want: true},
		{name: "跨零点区间不含终点", conditions: `{"time":{"ranges":[["22:00","06:00"]]}}`, at: at(6, 0), want: false},
		{name: "跨零点区间不含白天", conditions: `{"time":{"ranges":[["22:00","06:00"]]}}`, at: at(12, 0), want: false},
		// 非法写法一律判不命中：反过来默认命中会把条件规则退化成全天兜底规则。
		{name: "未知时区判不命中", conditions: `{"time":{"tz":"Mars/Olympus","ranges":[["09:00","12:00"]]}}`, at: at(10, 0), want: false},
		{name: "时段未补零判不命中", conditions: `{"time":{"ranges":[["9:00","12:00"]]}}`, at: at(10, 0), want: false},
		{name: "星期越界判不命中", conditions: `{"time":{"weekdays":[8]}}`, at: at(10, 0), want: false},
		{name: "时段项数不足判不命中", conditions: `{"time":{"ranges":[["09:00"]]}}`, at: at(10, 0), want: false},
	}
	for _, item := range cases {
		if got := RuleMatches(item.conditions, "", TokenUsage{}, item.at); got != item.want {
			t.Errorf("%s：命中 = %t，期望 %t", item.name, got, item.want)
		}
	}
}

// TestRuleBreakdownPicksRateByTimeWindow 确认时段命中的是费率本身，
// 而不只是布尔判定：同一规则在不同时刻算出不同单价。
func TestRuleBreakdownPicksRateByTimeWindow(t *testing.T) {
	location := shanghaiLocation(t)
	rates := `{"inputPerMillion":0.3,"outputPerMillion":1.2}`
	input, output := uint64(1_000_000), uint64(1_000_000)

	breakdown, matched := RuleBreakdown(peakTimeCondition, rates, "", TokenUsage{Input: &input, Output: &output}, time.Date(2026, 9, 14, 10, 0, 0, 0, location))
	if !matched || breakdown == nil {
		t.Fatal("高峰时刻应命中高价区规则")
	}
	if !breakdown.Cost().Equal(decimal.RequireFromString("1.5")) {
		t.Fatalf("高峰时刻成本 = %s，期望 1.5（0.3 + 1.2）", breakdown.Cost())
	}

	if _, matched := RuleBreakdown(peakTimeCondition, rates, "", TokenUsage{Input: &input, Output: &output}, time.Date(2026, 9, 14, 18, 55, 0, 0, location)); matched {
		t.Fatal("18:55 已过高价区时段，不应命中高价区规则")
	}
}

// deepseekPeakCondition 是价格同步按 BaseLLM billing_expr 生成的高峰档条件原文：
// 上游用 UTC 的周一至周五 01:00-04:00、06:00-10:00 两段；对应的非高峰档不带
// 任何条件（补集无法用「与」条件表达），靠兜底顺序在时段外生效。
const deepseekPeakCondition = `{"time":{"tz":"UTC","weekdays":[1,2,3,4,5],"ranges":[["01:00","04:00"],["06:00","10:00"]]}}`

// TestRuleMatchesDeepseekUpstreamPeakWindow 锁住同步出来的峰谷条件语义：
// 上游写 UTC，判定必须按 UTC，不能跟着容器本地时区（通常也是 UTC，但
// 一旦容器改成东八区就会整体偏移）漂移。
func TestRuleMatchesDeepseekUpstreamPeakWindow(t *testing.T) {
	at := func(year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	}
	cases := []struct {
		name string
		at   time.Time
		peak bool
	}{
		// 2026-09-14 周一、09-19 周六。
		{name: "UTC 周一 01:00 时段起点", at: at(2026, 9, 14, 1, 0), peak: true},
		{name: "UTC 周一 03:59 第一段内", at: at(2026, 9, 14, 3, 59), peak: true},
		{name: "UTC 周一 04:00 不含终点", at: at(2026, 9, 14, 4, 0), peak: false},
		{name: "UTC 周一 05:59 两段之间", at: at(2026, 9, 14, 5, 59), peak: false},
		{name: "UTC 周一 06:00 第二段起点", at: at(2026, 9, 14, 6, 0), peak: true},
		{name: "UTC 周一 09:59 第二段内", at: at(2026, 9, 14, 9, 59), peak: true},
		{name: "UTC 周一 10:00 不含终点", at: at(2026, 9, 14, 10, 0), peak: false},
		{name: "UTC 周一 00:59 时段之前", at: at(2026, 9, 14, 0, 59), peak: false},
		{name: "UTC 周六 02:00 不属工作日", at: at(2026, 9, 19, 2, 0), peak: false},
		{name: "UTC 周日 07:00 不属工作日", at: at(2026, 9, 20, 7, 0), peak: false},
	}
	for _, item := range cases {
		if got := RuleMatches(deepseekPeakCondition, "", TokenUsage{}, item.at); got != item.peak {
			t.Errorf("%s：高峰档命中 = %t，期望 %t", item.name, got, item.peak)
		}
		// 兜底档无条件，任何时刻都必须命中，否则时段外会静默不计费。
		if !RuleMatches(`{}`, "", TokenUsage{}, item.at) {
			t.Errorf("%s：兜底档应恒命中", item.name)
		}
	}
}

// TestRuleBreakdownUsesPeakRateInsideUpstreamWindow 确认时段内真的按高峰价计费。
func TestRuleBreakdownUsesPeakRateInsideUpstreamWindow(t *testing.T) {
	input, output := uint64(1_000_000), uint64(1_000_000)
	tokens := TokenUsage{Input: &input, Output: &output}
	peakRates := `{"cachedInputPerMillion":0.006,"inputPerMillion":0.3,"outputPerMillion":1.2}`
	offPeakRates := `{"cachedInputPerMillion":0.003,"inputPerMillion":0.15,"outputPerMillion":0.6}`
	peakAt := time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC)
	offPeakAt := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)

	peak, matched := RuleBreakdown(deepseekPeakCondition, peakRates, "", tokens, peakAt)
	if !matched || peak == nil {
		t.Fatal("高峰时刻应命中高峰档")
	}
	if !peak.Cost().Equal(decimal.RequireFromString("1.5")) {
		t.Fatalf("高峰时刻成本 = %s，期望 1.5（0.3 + 1.2）", peak.Cost())
	}
	if _, matched := RuleBreakdown(deepseekPeakCondition, peakRates, "", tokens, offPeakAt); matched {
		t.Fatal("非高峰时刻不应命中高峰档，应落到兜底档")
	}
	offPeak, matched := RuleBreakdown(`{}`, offPeakRates, "", tokens, offPeakAt)
	if !matched || offPeak == nil {
		t.Fatal("非高峰时刻应命中兜底档")
	}
	if !offPeak.Cost().Equal(decimal.RequireFromString("0.75")) {
		t.Fatalf("非高峰时刻成本 = %s，期望 0.75（0.15 + 0.6）", offPeak.Cost())
	}
}
