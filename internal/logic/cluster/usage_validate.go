package cluster

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
)

const usageBatchMaxRows = 1000

// 固定白名单既阻止保留列/SQL表达式注入，也避免 ORM 静默过滤未知字段。
var usageStringLimits = map[string]int{
	"request_id": 64, "endpoint": 64, "upstream_endpoint": 64,
	"protocol_conversion": 64, "client_ip": 45, "ip_location": 255,
	"requested_model": 191, "upstream_model": 191, "reasoning_effort": 32,
	"billing_details_json": 65535, "attempt_flow_json": 65535, "error_message": 1024,
}

var usageIntegerLimits = map[string]uint64{
	"user_id": math.MaxInt64, "api_key_id": math.MaxInt64,
	"channel_id": math.MaxInt64, "channel_credential_id": math.MaxInt64,
	"http_status": 65535, "is_stream": 1, "attempts": 255,
	"input_tokens": math.MaxInt64, "cached_input_tokens": math.MaxInt64,
	"output_tokens": math.MaxInt64, "total_tokens": math.MaxInt64,
	"duration_ms": math.MaxInt64, "first_token_ms": math.MaxInt64,
	"health_score_at_request": 100,
}

func validateUsageBatch(input usageBatch) ([]map[string]any, error) {
	if !usageIdentity(input.BatchID, 64) {
		return nil, gerror.New("cluster: invalid usage batch id")
	}
	if len(input.Rows) > usageBatchMaxRows {
		return nil, gerror.Newf("cluster: usage batch exceeds %d rows", usageBatchMaxRows)
	}
	var (
		rows = make([]map[string]any, 0, len(input.Rows))
		seen = make(map[string]struct{}, len(input.Rows))
	)
	for index, row := range input.Rows {
		clean, err := validateUsageRow(row)
		if err != nil {
			return nil, gerror.Wrapf(err, "validate usage row %d", index)
		}
		id := clean["request_id"].(string)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		rows = append(rows, clean)
	}
	return rows, nil
}

func validateUsageRow(row map[string]any) (map[string]any, error) {
	clean := make(map[string]any, len(row))
	for key, value := range row {
		if limit, ok := usageStringLimits[key]; ok {
			if value == nil && key != "request_id" {
				continue
			}
			text, ok := value.(string)
			if !ok || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit || strings.ContainsRune(text, '\x00') {
				return nil, gerror.Newf("cluster: invalid usage string %s", key)
			}
			if (key == "billing_details_json" || key == "attempt_flow_json") && len(text) > limit {
				return nil, gerror.Newf("cluster: usage JSON %s too large", key)
			}
			clean[key] = text
			continue
		}
		if limit, ok := usageIntegerLimits[key]; ok {
			if value == nil && key != "user_id" && key != "api_key_id" {
				continue
			}
			if pointer, ok := value.(*int); ok && key == "health_score_at_request" {
				if pointer == nil {
					continue
				}
				value = *pointer
			}
			number, err := usageUint(value)
			if err != nil || number > limit {
				return nil, gerror.Newf("cluster: invalid usage integer %s", key)
			}
			clean[key] = number
			continue
		}
		switch key {
		case "estimated_cost":
			amount, err := usageAmount(value)
			if err != nil {
				return nil, err
			}
			// 与 DECIMAL(20,8) 存储值一致，账单和扣费使用同一精度。
			clean[key] = amount.StringFixed(8)
		case "created_at":
			var stamp time.Time
			switch typed := value.(type) {
			case time.Time:
				stamp = typed
			case string:
				var err error
				stamp, err = time.Parse(time.RFC3339Nano, typed)
				if err != nil {
					return nil, gerror.New("cluster: invalid usage created_at")
				}
			default:
				return nil, gerror.New("cluster: invalid usage created_at")
			}
			if stamp.Year() < 1000 || stamp.Year() > 9999 {
				return nil, gerror.New("cluster: invalid usage created_at range")
			}
			clean[key] = stamp
		default:
			return nil, gerror.Newf("cluster: forbidden usage field %s", key)
		}
	}
	id, _ := clean["request_id"].(string)
	if !usageIdentity(id, 64) {
		return nil, gerror.New("cluster: invalid usage request id")
	}
	for _, key := range []string{"user_id", "api_key_id"} {
		if uintValueOf(clean[key]) == 0 {
			return nil, gerror.Newf("cluster: missing usage identity %s", key)
		}
	}
	charged, err := usageCharged(clean["billing_details_json"])
	if err != nil {
		return nil, err
	}
	if _, exists := clean["estimated_cost"]; !exists {
		if charged {
			return nil, gerror.New("cluster: charged usage missing cost")
		}
		clean["estimated_cost"] = "0.00000000"
	}
	return clean, nil
}

func usageIdentity(text string, limit int) bool {
	if text == "" || len(text) > limit {
		return false
	}
	for _, char := range text {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-_.:", char)) {
			return false
		}
	}
	return true
}

func usageUint(value any) (uint64, error) {
	var text string
	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case int, int32, int64, uint, uint32, uint64:
		text = fmt.Sprint(typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < 0 || typed > 1<<53 || math.Trunc(typed) != typed {
			return 0, gerror.New("cluster: invalid integer")
		}
		text = strconv.FormatFloat(typed, 'f', 0, 64)
	default:
		return 0, gerror.New("cluster: invalid integer type")
	}
	number, err := strconv.ParseUint(text, 10, 63)
	return number, gerror.Wrap(err, "cluster: invalid unsigned integer")
}

func usageAmount(value any) (decimal.Decimal, error) {
	var text string
	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case string: // 已验证并归一化的内部行也走此入口。
		text = typed
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return decimal.Zero, gerror.New("cluster: non-finite usage cost")
		}
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	case int, int64, uint64:
		text = fmt.Sprint(typed)
	default:
		return decimal.Zero, gerror.New("cluster: invalid usage cost type")
	}
	// 防止恶意指数导致 decimal 分配巨量内存。
	if len(text) > 64 {
		return decimal.Zero, gerror.New("cluster: invalid usage cost format")
	}
	if parts := strings.FieldsFunc(text, func(r rune) bool { return r == 'e' || r == 'E' }); len(parts) > 1 {
		exponent, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil || exponent < -32 || exponent > 12 {
			return decimal.Zero, gerror.New("cluster: invalid usage cost exponent")
		}
	}
	amount, err := decimal.NewFromString(text)
	if err != nil || amount.IsNegative() || amount.GreaterThan(decimal.RequireFromString("999999999999.99999999")) {
		return decimal.Zero, gerror.New("cluster: invalid usage cost range")
	}
	return amount.Round(8), nil
}

func usageCharged(value any) (bool, error) {
	if value == nil || value == "" {
		return false, nil // 旧版无明细记录仅统计，绝不推断为已收费。
	}
	raw, ok := value.(string)
	if !ok {
		return false, gerror.New("cluster: invalid billing details type")
	}
	var details map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &details); err != nil || details == nil {
		return false, gerror.New("cluster: invalid billing details JSON")
	}
	flag, exists := details["charged"]
	if !exists {
		return false, nil
	}
	if string(flag) != "true" && string(flag) != "false" {
		return false, gerror.New("cluster: charged must be boolean")
	}
	return string(flag) == "true", nil
}
