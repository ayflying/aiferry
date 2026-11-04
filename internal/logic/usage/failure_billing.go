package usage

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"
)

// FailedBillingDetails 是失败请求的价格快照输入；费用归零，价格元数据保留。
type FailedBillingDetails struct {
	BillingMode string
	Currency    string
	Rule        *BillingRuleSnapshot
	Items       []BillingItem
	Subtotal    string
	Total       string
}

// FailureCost 返回失败请求的落库费用。失败不产生费用，也不使用非零哨兵。
func FailureCost() decimal.Decimal {
	return decimal.Zero
}

// NewFailedBillingDetails 保留原套餐、计费模式和费率，包括零用量项。
// 数量保留供排障，但所有金额为零且不扣款；不修改调用方的原快照。
func NewFailedBillingDetails(input *FailedBillingDetails) *BillingBreakdown {
	if input == nil || (len(input.Items) == 0 && input.Rule == nil) {
		return nil
	}
	items := make([]BillingItem, len(input.Items))
	for index, item := range input.Items {
		item.Amount = "0"
		items[index] = item
	}
	currency := input.Currency
	if currency == "" {
		currency = "USD"
	}
	mode := input.BillingMode
	if mode == "" {
		mode = "token"
	}
	var rule *BillingRuleSnapshot
	if input.Rule != nil {
		snapshot := *input.Rule
		rule = &snapshot
	}
	return &BillingBreakdown{
		BillingMode: mode,
		Currency:    currency,
		Rule:        rule,
		Charged:     false,
		Items:       items,
		Subtotal:    "0",
		Total:       "0.00000000",
	}
}

// reconstructFailureBillingDetails 按日志的模型、端点和请求时刻回填失败价格。
// 无快照时不依赖旧 estimated_cost 是否存在；只补展示信息，不回写金额或余额。
func (s *sUsage) reconstructFailureBillingDetails(ctx context.Context, items []LogView) {
	reconstructFailureDetails(ctx, items, func(ctx context.Context, item LogView) *BillingBreakdown {
		endpoint := item.UpstreamEndpoint
		if endpoint == "" {
			endpoint = strings.TrimPrefix(item.Endpoint, "test:")
		}
		return estimatePublicModelBreakdown(ctx, item.RequestedModel, endpoint, tokenUsageFromLog(item), item.CreatedAt, true)
	})
}

func reconstructFailureDetails(ctx context.Context, items []LogView, estimate func(context.Context, LogView) *BillingBreakdown) {
	for index := range items {
		item := &items[index]
		if item.BillingDetails != nil || !isFailureLog(*item) {
			continue
		}
		original := estimate(ctx, *item)
		if original == nil {
			continue
		}
		breakdown := NewFailedBillingDetails(&FailedBillingDetails{
			BillingMode: original.BillingMode, Currency: original.Currency,
			Rule: original.Rule, Items: original.Items,
		})
		if breakdown != nil {
			breakdown.Reconstructed = true
			item.BillingDetails = breakdown
		}
	}
}

// isFailureLog 与全站成功率保持一致：非 2xx 都是失败。
func isFailureLog(item LogView) bool {
	return item.HttpStatus < 200 || item.HttpStatus >= 300
}
