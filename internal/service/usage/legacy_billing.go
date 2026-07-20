package usage

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *Service) reconstructLegacyBillingDetails(ctx context.Context, items []LogView) {
	for index := range items {
		item := &items[index]
		if item.BillingDetails != nil || item.EstimatedCost == nil || item.HttpStatus < 200 || item.HttpStatus >= 300 {
			continue
		}
		breakdown := EstimatePublicModelBreakdown(ctx, item.RequestedModel, item.UpstreamEndpoint, tokenUsageFromLog(*item))
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
