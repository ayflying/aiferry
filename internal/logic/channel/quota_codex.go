package channel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// codexUsageWindow 对应官方 RateLimitWindowSnapshot。reset_at 是 Unix 秒，
// 不是其他套餐接口使用的毫秒；百分比不代表金额、token 或剩余调用次数。
type codexUsageWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds int64    `json:"limit_window_seconds"`
	ResetAt            int64    `json:"reset_at"`
	ResetAfterSeconds  *int64   `json:"reset_after_seconds"`
}

type codexUsageLimits struct {
	Primary   *codexUsageWindow `json:"primary_window"`
	Secondary *codexUsageWindow `json:"secondary_window"`
}

func parseCodexUsage(body []byte) (QuotaView, error) {
	var payload struct {
		PlanType         string            `json:"plan_type"`
		RateLimit        *codexUsageLimits `json:"rate_limit"`
		CodeReviewLimit  *codexUsageLimits `json:"code_review_rate_limit"`
		AdditionalLimits []struct {
			LimitName      string            `json:"limit_name"`
			MeteredFeature string            `json:"metered_feature"`
			RateLimit      *codexUsageLimits `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return QuotaView{}, gerror.Wrap(err, "decode codex usage response")
	}
	view := QuotaView{
		Mode: channeltype.AdapterCodexUsage, Level: strings.TrimSpace(payload.PlanType), QueriedAt: time.Now(),
	}
	appendCodexUsageLimits(&view, "codex", "Codex", payload.RateLimit)
	// 兼容返回独立代码审查额度的上游版本，不混入普通 Codex 用量窗口。
	appendCodexUsageLimits(&view, "codex_code_review", "代码审查", payload.CodeReviewLimit)
	for index, additional := range payload.AdditionalLimits {
		feature := strings.TrimSpace(additional.MeteredFeature)
		if feature == "" {
			feature = fmt.Sprintf("unknown_%d", index)
		}
		label := strings.TrimSpace(additional.LimitName)
		if label == "" {
			label = feature
		}
		appendCodexUsageLimits(&view, "codex_feature_"+feature, label, additional.RateLimit)
	}
	if len(view.Windows) == 0 {
		// 不回显完整 WHAM 载荷，避免账户信息或未来新增敏感字段进入错误日志。
		return QuotaView{}, gerror.New("上游未返回可用的 Codex 套餐额度窗口")
	}
	return view, nil
}

func appendCodexUsageLimits(view *QuotaView, kind, label string, limits *codexUsageLimits) {
	if limits == nil {
		return
	}
	appendCodexUsageWindow(view, kind+"_primary", label+" 主窗口", limits.Primary)
	appendCodexUsageWindow(view, kind+"_secondary", label+" 次窗口", limits.Secondary)
}

func appendCodexUsageWindow(view *QuotaView, kind, label string, source *codexUsageWindow) {
	if source == nil || source.UsedPercent == nil {
		return
	}
	if source.LimitWindowSeconds > 0 {
		// 合并账号时不同持续时间不能当成同一个窗口，尤其不能把主窗口强认作 5 小时。
		kind += fmt.Sprintf("_%d", source.LimitWindowSeconds)
		label += "（" + codexWindowDuration(source.LimitWindowSeconds) + "）"
	}
	window := QuotaWindow{Kind: kind, Label: label, UsedPercent: quotaPercent(source.UsedPercent)}
	if source.ResetAt > 0 {
		reset := time.Unix(source.ResetAt, 0)
		window.NextResetAt = &reset
	} else if source.ResetAfterSeconds != nil && *source.ResetAfterSeconds >= 0 {
		// 用 Unix 秒相加避免把不可信秒数转为 time.Duration 时溢出。
		seconds := *source.ResetAfterSeconds
		if seconds <= (1<<63-1)-view.QueriedAt.Unix() {
			reset := time.Unix(view.QueriedAt.Unix()+seconds, int64(view.QueriedAt.Nanosecond()))
			window.NextResetAt = &reset
		}
	}
	view.Windows = append(view.Windows, window)
}

func codexWindowDuration(seconds int64) string {
	switch {
	case seconds%(7*24*60*60) == 0:
		return fmt.Sprintf("%d 周", seconds/(7*24*60*60))
	case seconds%(24*60*60) == 0:
		return fmt.Sprintf("%d 天", seconds/(24*60*60))
	case seconds%(60*60) == 0:
		return fmt.Sprintf("%d 小时", seconds/(60*60))
	case seconds%60 == 0:
		return fmt.Sprintf("%d 分钟", seconds/60)
	default:
		return fmt.Sprintf("%d 秒", seconds)
	}
}
