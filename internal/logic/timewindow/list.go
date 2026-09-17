package timewindow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 时间窗列表是「定时关闭时段」的多规则形态：列表内任一窗口命中即视为处于关闭时段。
//
// 存储格式演进：旧版 closed_windows_json 只存单个窗口对象，多规则升级后改存窗口
// 数组。ParseList 同时兼容两种形态——单对象自动包装成单元素列表，存量数据无需
// 迁移；写入侧统一走 NormalizeWindows 输出数组。
//
// 与单窗口函数的分界：Parse/Normalize/Contains/Allows 保持原语义供分时定价等
// 单窗口场景使用；关闭时段链路一律走本文件的列表版本。

// ParseList 解析存储中的时间窗列表 JSON，兼容三种形态：
// 空串与 "null" 视为未配置返回 nil；JSON 数组视为窗口列表；
// 旧版单对象自动包装成单元素列表。
func ParseList(raw string) ([]Window, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if !strings.HasPrefix(trimmed, "[") {
		window, err := Parse(trimmed)
		if err != nil {
			return nil, err
		}
		return []Window{window}, nil
	}
	var windows []Window
	if err := json.Unmarshal([]byte(trimmed), &windows); err != nil {
		return nil, fmt.Errorf("时间窗列表 JSON 无效：%w", err)
	}
	for index := range windows {
		windows[index].TZ = strings.TrimSpace(windows[index].TZ)
		if err := windows[index].Validate(); err != nil {
			return nil, err
		}
	}
	return windows, nil
}

// NormalizeWindows 校验并归一化一个时间窗列表，返回存储用 JSON（数组形态）。
// 列表内等价于「不限制」的窗口被剔除；剔除后为空时返回空串，调用方据此清空列。
func NormalizeWindows(windows []Window) (string, error) {
	normalized := make([]Window, 0, len(windows))
	for _, window := range windows {
		if err := window.Validate(); err != nil {
			return "", err
		}
		if window.IsZero() {
			continue
		}
		window.Weekdays = normalizeWeekdays(window.Weekdays)
		window.Ranges = normalizeRanges(window.Ranges)
		normalized = append(normalized, window)
	}
	if len(normalized) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("时间窗 JSON 编码失败：%w", err)
	}
	return string(encoded), nil
}

// AnyContains 判断给定时刻是否落在列表中任一时间窗内；空列表恒为 false。
func AnyContains(windows []Window, at time.Time) bool {
	for _, window := range windows {
		if window.Contains(at) {
			return true
		}
	}
	return false
}
