package channel

import (
	"context"
	"database/sql"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/service/system"
)

const credentialZeroBalanceReason = "费用查询返回余额为 0"

type credentialCostAvailabilityAction uint8

const (
	credentialCostAvailabilityNoChange credentialCostAvailabilityAction = iota
	credentialCostAvailabilityDisable
	credentialCostAvailabilityRecover
)

func credentialCostAvailabilityFor(remaining *float64) credentialCostAvailabilityAction {
	if remaining == nil {
		return credentialCostAvailabilityNoChange
	}
	if *remaining <= 0 {
		return credentialCostAvailabilityDisable
	}
	return credentialCostAvailabilityRecover
}

func credentialRecoveryData() do.ChannelCredentials {
	return do.ChannelCredentials{
		Status:                 1,
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}
}

func storedCostAllowsCredentialRecovery(remaining float64, autoDisabledAt, lastCostAt time.Time) bool {
	return remaining > 0 && !autoDisabledAt.IsZero() && !lastCostAt.IsZero() && !lastCostAt.Before(autoDisabledAt)
}

// syncCredentialAvailabilityFromCost keeps a credential's routing state aligned
// with a successful, credential-scoped balance query. A manual stop has no
// auto_disabled_at value, so a positive balance can never re-enable it.
func (s *Service) syncCredentialAvailabilityFromCost(ctx context.Context, credentialID uint64, remaining *float64) error {
	var (
		action = credentialCostAvailabilityFor(remaining)
		result sql.Result
		err    error
	)
	switch action {
	case credentialCostAvailabilityNoChange:
		return nil
	case credentialCostAvailabilityDisable:
		result, err = dao.ChannelCredentials.Ctx(ctx).
			Where(do.ChannelCredentials{Id: credentialID, Status: 1}).
			Data(do.ChannelCredentials{
				Status:                 0,
				AutoDisabledAt:         gtime.Now(),
				AutoDisabledReason:     credentialZeroBalanceReason,
				AutoDisabledStatusCode: gdb.Raw("NULL"),
				AutoDisabledSource:     system.AutoDisableSourceCostQuery,
			}).Update()
	case credentialCostAvailabilityRecover:
		result, err = dao.ChannelCredentials.Ctx(ctx).
			Where(do.ChannelCredentials{Id: credentialID, Status: 0}).
			WhereNotNull(dao.ChannelCredentials.Columns().AutoDisabledAt).
			Data(credentialRecoveryData()).Update()
	}
	if err != nil {
		return gerror.Wrap(err, "synchronize credential availability from cost")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil
	}
	s.clearCredentialTransient(ctx, credentialID)
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}

// reconcileCredentialAvailabilityFromStoredCost catches a successful balance
// refresh that completed after the process handling it was interrupted. It only
// restores credentials still marked as automatically disabled and whose latest
// positive balance is at least as new as the disabling event.
func (s *Service) reconcileCredentialAvailabilityFromStoredCost(ctx context.Context) error {
	type storedCredentialCost struct {
		Id              uint64    `orm:"id"`
		AutoDisabledAt  time.Time `orm:"auto_disabled_at"`
		LastCostAt      time.Time `orm:"last_cost_at"`
		LastCostBalance float64   `orm:"last_cost_remaining"`
	}
	var rows []storedCredentialCost
	columns := dao.ChannelCredentials.Columns()
	if err := dao.ChannelCredentials.Ctx(ctx).
		Fields(columns.Id, columns.AutoDisabledAt, columns.LastCostAt, columns.LastCostRemaining).
		Where(do.ChannelCredentials{Status: 0}).
		WhereNotNull(columns.AutoDisabledAt).
		WhereNotNull(columns.LastCostAt).
		WhereNotNull(columns.LastCostRemaining).
		Scan(&rows); err != nil {
		return gerror.Wrap(err, "load automatically disabled credentials for balance recovery")
	}
	recovered := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if !storedCostAllowsCredentialRecovery(row.LastCostBalance, row.AutoDisabledAt, row.LastCostAt) {
			continue
		}
		result, err := dao.ChannelCredentials.Ctx(ctx).
			Where(do.ChannelCredentials{Id: row.Id, Status: 0}).
			WhereNotNull(columns.AutoDisabledAt).
			WhereGT(columns.LastCostRemaining, 0).
			Where("last_cost_at >= auto_disabled_at").
			Data(credentialRecoveryData()).Update()
		if err != nil {
			return gerror.Wrapf(err, "recover channel credential %d from stored balance", row.Id)
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			recovered = append(recovered, row.Id)
		}
	}
	if len(recovered) == 0 {
		return nil
	}
	for _, credentialID := range recovered {
		s.clearCredentialTransient(ctx, credentialID)
		s.resilience.ResetCredentialRecoverySchedule(ctx, credentialID)
	}
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}
