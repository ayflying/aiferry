package usage

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *sUsage) reconstructLegacyBillingDetails(ctx context.Context, items []LogView) {
	for index := range items {
		item := &items[index]
		// 成功记录才复原成正向账单；失败记录的计费快照由 reconstructFailureBillingDetails 处理，
		// 两者分开是为了不让「失败也有成本」这条新语义影响既有的成功记录校验。
		if item.BillingDetails != nil || item.EstimatedCost == nil || isFailureLog(*item) {
			continue
		}
		// 必须用日志自身的 created_at 作为时段评估时刻：分时定价下套用「当前时间」
		// 会算出与库中 estimated_cost 不符的金额，导致历史账单详情无法重建。
		breakdown := EstimatePublicModelBreakdown(ctx, item.RequestedModel, item.UpstreamEndpoint, tokenUsageFromLog(*item), item.CreatedAt)
		breakdown = verifiedLegacyBillingDetails(*item, breakdown)
		if breakdown == nil {
			continue
		}
		encoded, err := breakdown.JSON()
		if err != nil {
			continue
		}
		_, _ = dao.UsageLogs.Ctx(ctx).
			Where(dao.UsageLogs.Columns().Id, item.Id).
			WhereNull(dao.UsageLogs.Columns().BillingDetailsJson).
			Data(do.UsageLogs{BillingDetailsJson: encoded}).
			Update()
		item.BillingDetails = breakdown
	}
}

func verifiedLegacyBillingDetails(item LogView, breakdown *BillingBreakdown) *BillingBreakdown {
	if breakdown == nil || item.EstimatedCost == nil {
		return nil
	}
	recorded := decimal.NewFromFloat(*item.EstimatedCost).Round(8)
	if !breakdown.Cost().Equal(recorded) {
		return nil
	}
	breakdown.Charged = item.UserId != SystemUserID
	breakdown.Reconstructed = true
	return breakdown
}

func tokenUsageFromLog(item LogView) TokenUsage {
	return TokenUsage{
		Input: item.InputTokens, CachedInput: item.CachedInputTokens,
		Output: item.OutputTokens, Total: item.TotalTokens,
	}
}
