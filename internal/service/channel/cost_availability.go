package channel

import (
	"context"
	"database/sql"

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
			Data(do.ChannelCredentials{
				Status:                 1,
				AutoDisabledAt:         gdb.Raw("NULL"),
				AutoDisabledReason:     gdb.Raw("NULL"),
				AutoDisabledStatusCode: gdb.Raw("NULL"),
				AutoDisabledSource:     gdb.Raw("NULL"),
			}).Update()
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
