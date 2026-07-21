package channel

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

const costQueryAutoDisableSource = "cost_query"

type costQueryDisabledCredential struct {
	ID                 uint64    `orm:"id"`
	Status             int       `orm:"status"`
	AutoDisabledAt     time.Time `orm:"auto_disabled_at"`
	AutoDisabledSource string    `orm:"auto_disabled_source"`
}

func canRestoreCostQueryDisabledCredential(credential costQueryDisabledCredential) bool {
	return credential.Status == 0 && !credential.AutoDisabledAt.IsZero() && credential.AutoDisabledSource == costQueryAutoDisableSource
}

func costQueryCredentialRecoveryData() do.ChannelCredentials {
	return do.ChannelCredentials{
		Status:                 1,
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}
}

// RestoreCostQueryDisabledCredentials restores only credentials that were
// previously disabled by the removed balance-query rule. Cost data is for
// display and notification, not proof that a credential is unusable.
func (s *Service) RestoreCostQueryDisabledCredentials(ctx context.Context) error {
	credentials := make([]costQueryDisabledCredential, 0)
	columns := dao.ChannelCredentials.Columns()
	if err := dao.ChannelCredentials.Ctx(ctx).
		Fields(columns.Id, columns.Status, columns.AutoDisabledAt, columns.AutoDisabledSource).
		Where(do.ChannelCredentials{Status: 0, AutoDisabledSource: costQueryAutoDisableSource}).
		WhereNotNull(columns.AutoDisabledAt).
		OrderAsc(columns.Id).
		Scan(&credentials); err != nil {
		return gerror.Wrap(err, "load credentials disabled by cost query")
	}

	for _, credential := range credentials {
		if !canRestoreCostQueryDisabledCredential(credential) {
			continue
		}
		result, err := dao.ChannelCredentials.Ctx(ctx).
			Where(do.ChannelCredentials{Id: credential.ID, Status: 0, AutoDisabledSource: costQueryAutoDisableSource}).
			WhereNotNull(columns.AutoDisabledAt).
			Data(costQueryCredentialRecoveryData()).
			Update()
		if err != nil {
			return gerror.Wrapf(err, "restore channel credential %d disabled by cost query", credential.ID)
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			continue
		}
		s.clearCredentialTransient(ctx, credential.ID)
		s.resilience.ResetCredentialRecoverySchedule(ctx, credential.ID)
	}

	// The migration can restore rows before this process starts. Clear the
	// shared cache regardless so its 24-hour snapshot cannot retain old status.
	s.InvalidateListCache(ctx)
	if err := s.invalidateRoutes(ctx); err != nil {
		return gerror.Wrap(err, "invalidate routes after restoring cost query credentials")
	}
	return nil
}
