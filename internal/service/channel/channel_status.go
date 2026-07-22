package channel

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *Service) SetStatus(ctx context.Context, channelID uint64, status int) error {
	if _, err := s.Get(ctx, channelID); err != nil {
		return err
	}

	status = boolStatus(status)
	credentialIDs := make([]uint64, 0)
	err := dao.Channels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		channelData := do.Channels{
			Status:                 status,
			AutoDisabledAt:         gdb.Raw("NULL"),
			AutoDisabledReason:     gdb.Raw("NULL"),
			AutoDisabledStatusCode: gdb.Raw("NULL"),
			AutoDisabledSource:     gdb.Raw("NULL"),
		}
		if _, err := dao.Channels.Ctx(txCtx).Where(do.Channels{Id: channelID}).Data(channelData).Update(); err != nil {
			return gerror.Wrap(err, "update channel status")
		}
		if status == 0 {
			return nil
		}
		if err := dao.ChannelCredentials.Ctx(txCtx).
			Fields(dao.ChannelCredentials.Columns().Id).
			Where(do.ChannelCredentials{ChannelId: channelID}).
			Scan(&credentialIDs); err != nil {
			return gerror.Wrap(err, "list channel credentials for recovery")
		}
		if _, err := dao.ChannelCredentials.Ctx(txCtx).Where(do.ChannelCredentials{ChannelId: channelID}).Data(do.ChannelCredentials{
			Status:                 1,
			AutoDisabledAt:         gdb.Raw("NULL"),
			AutoDisabledReason:     gdb.Raw("NULL"),
			AutoDisabledStatusCode: gdb.Raw("NULL"),
			AutoDisabledSource:     gdb.Raw("NULL"),
		}).Update(); err != nil {
			return gerror.Wrap(err, "recover channel credentials")
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.resilience.ClearChannelAutoDisableFailures(ctx, channelID)
	s.resilience.ResetChannelRecoverySchedule(ctx, channelID)
	for _, credentialID := range credentialIDs {
		s.clearCredentialTransient(ctx, credentialID)
		s.resilience.ResetCredentialRecoverySchedule(ctx, credentialID)
	}
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}
