package usage

import (
	"context"

	"github.com/shopspring/decimal"
)

// failureCostSentinel 是「失败但按量计费过」的标记价。
//
// 为什么不能用 0：estimated_cost 为 0 在库里和「这条记录没解析到用量」完全同形，
// 一旦拿 0 当记号，成功的免费调用、历史脏数据都会被一起改写成计费快照。
// 用一个不可能出现的极小正数做标记，判断成本极低，且不会和真实金额混淆。
var failureCostSentinel = decimal.RequireFromString("1e-9")

// failureBillingMode 标记这类快照来自失败的请求，前端据此显示「已计费（失败）」而不是普通计费。
const failureBillingMode = "failed"

// FailedBillingDetails 是失败请求的计费快照输入。
type FailedBillingDetails struct {
	Currency string
	Items    []BillingItem
	Subtotal string
	Total    string
}

// FailureCost 返回写库用的标记价：失败的计费记录在库里表现为「非 0 但没有真实金额」。
func FailureCost() decimal.Decimal {
	return failureCostSentinel
}

// NewFailedBillingDetails 把失败请求的计费快照（通常由请求侧按失败时的用量估算得到）
// 转成可直接落库的 BillingBreakdown。
//
// 失败请求不受「已写出内容与否」限制：只要上游确实消耗了 token 并且有价格，
// 这次调用就有成本，哪怕客户端拿到的是错误。所以这条路径刻意跳过
// reconstructLegacyBillingDetails 里「只有 2xx 才复原」的守卫，
// 并在快照上显式写明：未扣费（失败请求不倒扣用户余额）、但已计费。
func NewFailedBillingDetails(input *FailedBillingDetails) *BillingBreakdown {
	if input == nil {
		return nil
	}
	items := make([]BillingItem, 0, len(input.Items))
	total := decimal.Zero
	for _, item := range input.Items {
		// 上游没有返回用量时按量项数量为 0，这类项留着只会让人误以为算过。
		if item.Type != "request" && item.Quantity == 0 {
			continue
		}
		items = append(items, item)
		amount, err := decimal.NewFromString(item.Amount)
		if err != nil {
			continue
		}
		total = total.Add(amount)
	}
	if len(items) == 0 {
		return nil
	}
	currency := input.Currency
	if currency == "" {
		currency = "USD"
	}
	subtotal := input.Subtotal
	if subtotal == "" {
		subtotal = total.String()
	}
	return &BillingBreakdown{
		BillingMode: failureBillingMode,
		Currency:    currency,
		// 失败请求不从用户账户扣款，charged 必须保持 false，否则详情弹窗会谎称扣了钱。
		Charged: false,
		Items:   items,
		Subtotal: subtotal,
		Total:   total.StringFixed(8),
	}
}

// reconstructFailureBillingDetails 回填「失败但已计费」历史记录的计费快照。
//
// 只处理带标记价的失败记录：这些记录在库里已经是「有成本」的，
// 重建只会让详情页显示当时的价格明细，不会改变任何金额或余额。
func (s *sUsage) reconstructFailureBillingDetails(ctx context.Context, items []LogView) {
	for index := range items {
		item := &items[index]
		if item.BillingDetails != nil || item.EstimatedCost == nil || !isFailureLog(*item) {
			continue
		}
		// 与成功记录的复原保持一致：一律用日志自身的 created_at 评估时段价格。
		breakdown := EstimatePublicModelBreakdown(ctx, item.RequestedModel, item.UpstreamEndpoint, tokenUsageFromLog(*item), item.CreatedAt)
		if breakdown == nil {
			continue
		}
		breakdown.Charged = false
		breakdown.Reconstructed = true
		item.BillingDetails = breakdown
	}
}

// isFailureLog 把「失败」定义成状态码不在 2xx：这是全站统一的判定口径
// （成功率、失败提示、详情按钮都按它算），失败计费也必须用同一把尺子。
func isFailureLog(item LogView) bool {
	return item.HttpStatus < 200 || item.HttpStatus >= 300
}
