package channel

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *Service) saveCredentialCostResult(ctx context.Context, credentialID uint64, result CostResult) error {
	snapshot := do.ChannelCredentialCostSnapshots{
		ChannelCredentialId: credentialID,
		Mode:                result.Mode,
		Currency:            result.Currency,
		QueriedAt:           result.QueriedAt,
	}
	update := do.ChannelCredentials{LastCostCurrency: result.Currency, LastCostAt: result.QueriedAt}
	if result.UsedAmount == nil {
		snapshot.UsedAmount = gdb.Raw("NULL")
		update.LastCostUsed = gdb.Raw("NULL")
	} else {
		snapshot.UsedAmount = *result.UsedAmount
		update.LastCostUsed = *result.UsedAmount
	}
	if result.RemainingAmount == nil {
		snapshot.RemainingAmount = gdb.Raw("NULL")
		update.LastCostRemaining = gdb.Raw("NULL")
	} else {
		snapshot.RemainingAmount = *result.RemainingAmount
		update.LastCostRemaining = *result.RemainingAmount
	}
	if result.PeriodStart != nil {
		snapshot.PeriodStart = *result.PeriodStart
	}
	if result.PeriodEnd != nil {
		snapshot.PeriodEnd = *result.PeriodEnd
	}
	if _, err := dao.ChannelCredentialCostSnapshots.Ctx(ctx).Data(snapshot).Insert(); err != nil {
		return gerror.Wrap(err, "save channel credential cost snapshot")
	}
	if _, err := dao.ChannelCredentials.Ctx(ctx).Where(dao.ChannelCredentials.Columns().Id, credentialID).Data(update).Update(); err != nil {
		return gerror.Wrap(err, "update channel credential cost snapshot")
	}
	return s.syncCredentialAvailabilityFromCost(ctx, credentialID, result.RemainingAmount)
}

func (s *Service) saveChannelCostResult(ctx context.Context, channelID uint64, result CostResult) error {
	snapshot := do.ChannelCostSnapshots{
		ChannelId: channelID,
		Mode:      result.Mode,
		Currency:  result.Currency,
		QueriedAt: result.QueriedAt,
	}
	channelUpdate := do.Channels{LastCostCurrency: result.Currency, LastCostAt: result.QueriedAt}
	if result.UsedAmount == nil {
		snapshot.UsedAmount = gdb.Raw("NULL")
		channelUpdate.LastCostUsed = gdb.Raw("NULL")
	} else {
		snapshot.UsedAmount = *result.UsedAmount
		channelUpdate.LastCostUsed = *result.UsedAmount
	}
	if result.RemainingAmount == nil {
		snapshot.RemainingAmount = gdb.Raw("NULL")
		channelUpdate.LastCostRemaining = gdb.Raw("NULL")
	} else {
		snapshot.RemainingAmount = *result.RemainingAmount
		channelUpdate.LastCostRemaining = *result.RemainingAmount
	}
	if result.PeriodStart != nil {
		snapshot.PeriodStart = *result.PeriodStart
	}
	if result.PeriodEnd != nil {
		snapshot.PeriodEnd = *result.PeriodEnd
	}
	if _, err := dao.ChannelCostSnapshots.Ctx(ctx).Data(snapshot).Insert(); err != nil {
		return gerror.Wrap(err, "save cost snapshot")
	}
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, channelID).Data(channelUpdate).Update(); err != nil {
		return gerror.Wrap(err, "update channel cost snapshot")
	}
	return nil
}
