package channel

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

type trackedChannelCost struct {
	Name      string   `orm:"name"`
	Remaining *float64 `orm:"last_cost_remaining"`
}

func (s *Service) ApplyUsageCost(ctx context.Context, channelID uint64, amount decimal.Decimal) error {
	amount = amount.Round(8)
	if channelID == 0 || amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	literal := amount.StringFixed(8)
	columns := dao.Channels.Columns()
	result, err := dao.Channels.Ctx(ctx).
		Where(columns.Id, channelID).
		Where("(last_cost_currency IS NULL OR last_cost_currency = '' OR UPPER(last_cost_currency) = 'USD')").
		Data(do.Channels{
			LastCostUsed:      gdb.Raw("COALESCE(last_cost_used, 0) + " + literal),
			LastCostRemaining: gdb.Raw("CASE WHEN last_cost_remaining IS NULL THEN NULL ELSE GREATEST(last_cost_remaining - " + literal + ", 0) END"),
			LastCostCurrency:  gdb.Raw("COALESCE(NULLIF(last_cost_currency, ''), 'USD')"),
			LastCostAt:        time.Now(),
		}).Update()
	if err != nil {
		return gerror.Wrap(err, "apply channel usage cost")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil
	}
	var state trackedChannelCost
	if err = dao.Channels.Ctx(ctx).
		Fields(columns.Name, columns.LastCostRemaining).
		Where(columns.Id, channelID).
		Scan(&state); err != nil {
		return gerror.Wrap(err, "load tracked channel cost")
	}
	if state.Remaining != nil && s.mail != nil {
		s.mail.NotifyChannelLowBalance(ctx, channelID, state.Name, *state.Remaining)
	}
	return nil
}
