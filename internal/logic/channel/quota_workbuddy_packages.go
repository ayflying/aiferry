package channel

import (
	"encoding/json"
	"strconv"
	"strings"
)

// 本文件负责从 WorkBuddy 积分响应里取出「套餐额度」对象。
//
// 新官网真实响应的 data.Packages 与 data.CreditGroups 是同一份额度的不同视图，
// 不可递归累加两者。data.Packages 存在时以它为唯一权威来源，空数组也不回退镜像。
// 只有缺少 Packages 时，才递归扫描兼容历史外壳与容量字段别名。
// /v2/billing/meter/get-user-resource 曾是候选路由，不能据此认定服务端可用，
// 也不能把兼容的字段形状当作该接口已验证的响应契约。
//
// 边界：只认容量类字段，不认金额；一个对象至少要有「总量」或同时有「剩余+已用」
// 才算套餐，避免把其他业务对象里的散字段误当额度。

// workBuddyCapacityFields 定义三个容量字段的别名集合。按出现顺序匹配，
// 命中第一个即返回，避免同一对象里多个别名（如 TotalCapacity 与 totalCredit）
// 被重复累加。只收「带 Capacity / Credit 后缀」这类指向明确的写法，不收
// 裸露的 Total / Used ——那种词在其他业务对象里到处都是，会把无关字段当额度。
var workBuddyCapacityFields = map[string][]string{
	"code":   {"PackageCode", "packageCode", "package_code", "PackageName", "packageName", "package_name"},
	"total":  {"CycleTotalCapacity", "cycleTotalCapacity", "cycle_total_capacity", "TotalCapacity", "totalCapacity", "total_capacity", "TotalCredit", "totalCredit", "total_credit"},
	"remain": {"CycleRemainCapacity", "cycleRemainCapacity", "cycle_remain_capacity", "RemainCapacity", "remainCapacity", "remain_capacity", "RemainCredit", "remainCredit", "remain_credit", "RemainingCredit", "remainingCredit"},
	"used":   {"CycleUsedCapacity", "cycleUsedCapacity", "cycle_used_capacity", "UsedCapacity", "usedCapacity", "used_capacity", "UsedCredit", "usedCredit", "used_credit"},
}

// workBuddyPackageValues 是扫描出来的一份积分套餐，直接用数值表示，
// 不再回填 json.Number，避免两条解析路径各说一套。
type workBuddyPackageValues struct {
	PackageCode string
	Total       float64
	Remain      float64
	Used        float64
}

// collectWorkBuddyPackages 优先解析 data.Packages，缺失时才递归扫描响应体。
// JSON 无法解析时返回 nil，由调用方用原始片段报错。
func collectWorkBuddyPackages(body []byte) []workBuddyPackageValues {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	var found []workBuddyPackageValues
	if envelope, ok := root.(map[string]any); ok {
		if data, ok := envelope["data"].(map[string]any); ok {
			if packages, exists := data["Packages"]; exists {
				walkWorkBuddyNode(packages, &found)
				return dedupeWorkBuddyPackages(found)
			}
		}
	}
	walkWorkBuddyNode(root, &found)
	return dedupeWorkBuddyPackages(found)
}

func walkWorkBuddyNode(node any, out *[]workBuddyPackageValues) {
	switch value := node.(type) {
	case map[string]any:
		if pkg, ok := workBuddyPackageFromMap(value); ok {
			*out = append(*out, pkg)
		}
		for _, child := range value {
			walkWorkBuddyNode(child, out)
		}
	case []any:
		for _, child := range value {
			walkWorkBuddyNode(child, out)
		}
	}
}

// workBuddyPackageFromMap 判断一个 JSON 对象是否是积分套餐，并取出三个容量值。
func workBuddyPackageFromMap(node map[string]any) (workBuddyPackageValues, bool) {
	code, _ := workBuddyStringField(node, workBuddyCapacityFields["code"])
	total, hasTotal := workBuddyNumberField(node, workBuddyCapacityFields["total"])
	remain, hasRemain := workBuddyNumberField(node, workBuddyCapacityFields["remain"])
	used, hasUsed := workBuddyNumberField(node, workBuddyCapacityFields["used"])
	// 认定条件收紧一档：必须有总量，且至少还有剩余或已用其中之一；
	// 或者剩余与已用同时存在（个别套餐不下发总量）。
	if !(hasTotal && (hasRemain || hasUsed)) && !(hasRemain && hasUsed) {
		return workBuddyPackageValues{}, false
	}
	return workBuddyPackageValues{PackageCode: code, Total: total, Remain: remain, Used: used}, true
}

func workBuddyStringField(node map[string]any, names []string) (string, bool) {
	for _, name := range names {
		raw, ok := node[name]
		if !ok {
			continue
		}
		if text, ok := raw.(string); ok {
			return strings.TrimSpace(text), true
		}
		if number, ok := workBuddyFloat(raw); ok {
			return strconv.FormatFloat(number, 'f', -1, 64), true
		}
	}
	return "", false
}

func workBuddyNumberField(node map[string]any, names []string) (float64, bool) {
	for _, name := range names {
		raw, ok := node[name]
		if !ok {
			continue
		}
		if number, ok := workBuddyFloat(raw); ok {
			return number, true
		}
	}
	return 0, false
}

// workBuddyFloat 兼容三种上游写法：数字、数字字符串、布尔。
func workBuddyFloat(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(trimmed, 64)
		return number, err == nil
	case bool:
		if value {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// dedupeWorkBuddyPackages 去掉完全相同的重复套餐。递归扫描可能从不同层级
// 拿到同一份对象（例如外壳里同时有 packages 汇总和 packages[] 明细），
// 不去重会把积分算成两倍，余额一旦翻倍比查不到更有害。
func dedupeWorkBuddyPackages(items []workBuddyPackageValues) []workBuddyPackageValues {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	unique := make([]workBuddyPackageValues, 0, len(items))
	for _, item := range items {
		key := item.PackageCode + "|" +
			strconv.FormatFloat(item.Total, 'f', -1, 64) + "|" +
			strconv.FormatFloat(item.Remain, 'f', -1, 64) + "|" +
			strconv.FormatFloat(item.Used, 'f', -1, 64)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, item)
	}
	return unique
}
