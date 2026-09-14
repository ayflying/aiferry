// Package timewindow 提供「时区 + 星期 + 多时段」时间窗的解析、校验与命中判定。
//
// 同一套结构同时服务两类需求：
//   - 模型分时定价：在高峰时段使用更贵的费率（conditions.time）；
//   - 渠道模型定时关闭：在指定时段停止通过某渠道提供某个模型（closed_windows_json）。
//
// 线上容器本地时区通常是 UTC，因此未显式指定 tz 时统一按北京时间判定，
// 并内嵌 IANA 时区库，保证精简基础镜像也能解析 tz 名称。
package timewindow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	// 基础镜像可能不含 tzdata，内嵌一份避免 LoadLocation 失败。
	_ "time/tzdata"
)

// DefaultTimezone 是未显式指定时区时使用的时区。
const DefaultTimezone = "Asia/Shanghai"

const clockLayout = "15:04"

// Window 描述一个时间窗。
//
// 字段语义：
//   - TZ：IANA 时区名，空则取 DefaultTimezone；
//   - Weekdays：ISO 星期编号（1=周一 … 7=周日），空表示每天都生效；
//   - Ranges：一天内的时段，元素为 ["HH:MM","HH:MM"]；起止相同视为全天；
//     start > end 视为跨零点（例如 22:00–06:00）。
//
// 未配置的时间窗是零值（三个字段都为空），此时 HasRanges 为 false，
// Contains 恒为 false，即「不生效」。
type Window struct {
	TZ       string     `json:"tz,omitempty"`
	Weekdays []int      `json:"weekdays,omitempty"`
	Ranges   [][]string `json:"ranges,omitempty"`
}

// IsZero 判断时间窗是否等价于「不限制」，此时调用方应把配置清除而不是落库。
//
// 判据与前端 timeWindowIsEmpty 保持一致：只要没有任何时段，且星期要么没选、
// 要么七天全选，就等价于全天生效；只写了 tz 也算未配置——否则「清空关闭时段」
// 会退化成一条只带时区、实际不生效的脏记录。
func (w Window) IsZero() bool {
	if len(w.Ranges) > 0 {
		return false
	}
	weekdays := normalizeWeekdays(w.Weekdays)
	return len(weekdays) == 0 || len(weekdays) == 7
}

// Parse 解析存储中的时间窗 JSON。空串与 "null" 视为未配置，返回零值。
func Parse(raw string) (Window, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return Window{}, nil
	}
	var window Window
	if err := json.Unmarshal([]byte(trimmed), &window); err != nil {
		return Window{}, fmt.Errorf("时间窗 JSON 无效：%w", err)
	}
	window.TZ = strings.TrimSpace(window.TZ)
	if err := window.Validate(); err != nil {
		return Window{}, err
	}
	return window, nil
}

// Normalize 校验并归一化存储值：未配置的时间窗返回空串（调用方据此清空列），
// 已配置的返回排序去重后的紧凑 JSON。
func Normalize(raw string) (string, error) {
	window, err := Parse(raw)
	if err != nil {
		return "", err
	}
	if window.IsZero() {
		return "", nil
	}
	window.Weekdays = normalizeWeekdays(window.Weekdays)
	window.Ranges = normalizeRanges(window.Ranges)
	encoded, err := json.Marshal(window)
	if err != nil {
		return "", fmt.Errorf("时间窗 JSON 编码失败：%w", err)
	}
	return string(encoded), nil
}

// NormalizeWindow 校验并归一化一个已解析的时间窗，返回存储用 JSON。
func NormalizeWindow(window Window) (string, error) {
	if err := window.Validate(); err != nil {
		return "", err
	}
	if window.IsZero() {
		return "", nil
	}
	window.Weekdays = normalizeWeekdays(window.Weekdays)
	window.Ranges = normalizeRanges(window.Ranges)
	encoded, err := json.Marshal(window)
	if err != nil {
		return "", fmt.Errorf("时间窗 JSON 编码失败：%w", err)
	}
	return string(encoded), nil
}

// Validate 校验时间窗内容，供写入前拦截明显写错的条件。
func (w Window) Validate() error {
	if strings.TrimSpace(w.TZ) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(w.TZ)); err != nil {
			return fmt.Errorf("时间窗时区 %q 不是合法的 IANA 时区", w.TZ)
		}
	}
	for _, weekday := range w.Weekdays {
		if weekday < 1 || weekday > 7 {
			return fmt.Errorf("时间窗星期 %d 超出 1-7（周一至周日）范围", weekday)
		}
	}
	for _, item := range w.Ranges {
		if len(item) != 2 {
			return fmt.Errorf("时间窗时段必须是 [开始, 结束] 两个 HH:MM 值")
		}
		for _, value := range item {
			if _, err := parseClock(value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Location 返回判定使用的时区；tz 缺失或非法时回退 DefaultTimezone。
func (w Window) Location() *time.Location {
	if strings.TrimSpace(w.TZ) != "" {
		if location, err := time.LoadLocation(strings.TrimSpace(w.TZ)); err == nil {
			return location
		}
	}
	location, err := time.LoadLocation(DefaultTimezone)
	if err != nil {
		return time.UTC
	}
	return location
}

// Contains 判断给定时刻是否落在时间窗内。
func (w Window) Contains(at time.Time) bool {
	if len(w.Ranges) == 0 {
		return false
	}
	local := at.In(w.Location())
	if len(w.Weekdays) > 0 && !containsWeekday(w.Weekdays, isoWeekday(local)) {
		return false
	}
	minute := local.Hour()*60 + local.Minute()
	for _, item := range w.Ranges {
		start, startErr := parseClock(item[0])
		if startErr != nil {
			continue
		}
		end, endErr := parseClock(item[1])
		if endErr != nil {
			continue
		}
		if matchesClock(minute, start, end) {
			return true
		}
	}
	return false
}

// matchesClock 判断分钟数是否落在 [start, end) 区间内：
// 起止相同视为全天，start > end 视为跨零点。
func matchesClock(minute, start, end int) bool {
	switch {
	case start == end:
		return true
	case start < end:
		return minute >= start && minute < end
	default:
		return minute >= start || minute < end
	}
}

func parseClock(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	// time.Parse 对 "15:04" 宽松到接受 "9:00"，这里先做严格形态校验，
	// 保证存储与前端提交的都是补零的 HH:MM。
	if len(trimmed) != len(clockLayout) || trimmed[2] != ':' {
		return 0, fmt.Errorf("时间窗时段 %q 不是合法的 HH:MM", value)
	}
	parsed, err := time.Parse(clockLayout, trimmed)
	if err != nil {
		return 0, fmt.Errorf("时间窗时段 %q 不是合法的 HH:MM", value)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

// isoWeekday 返回 ISO 星期编号（1=周一 … 7=周日）。
func isoWeekday(at time.Time) int {
	weekday := int(at.Weekday())
	if weekday == 0 {
		return 7
	}
	return weekday
}

func containsWeekday(weekdays []int, weekday int) bool {
	for _, value := range weekdays {
		if value == weekday {
			return true
		}
	}
	return false
}

func normalizeWeekdays(weekdays []int) []int {
	if len(weekdays) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(weekdays))
	result := make([]int, 0, len(weekdays))
	for _, value := range weekdays {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func normalizeRanges(ranges [][]string) [][]string {
	if len(ranges) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ranges))
	result := make([][]string, 0, len(ranges))
	for _, item := range ranges {
		key := item[0] + "-" + item[1]
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, []string{item[0], item[1]})
	}
	return result
}
