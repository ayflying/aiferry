package channel

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) SetStatus(ctx context.Context, channelID uint64, status int) error {
	if err := s.ensureOwned(ctx, channelID); err != nil {
		return err
	}

	status = boolStatus(status)
	var previous entity.Channels
	if err := dao.Channels.Ctx(ctx).
		Fields(dao.Channels.Columns().Id, dao.Channels.Columns().Status).
		Where(do.Channels{Id: channelID}).
		Scan(&previous); err != nil {
		return gerror.Wrap(err, "load channel status before update")
	}
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
		// 手动启用渠道时同步恢复其被自动禁用的模型并重置健康评分。
		if _, err := dao.ChannelModels.Ctx(txCtx).Where(do.ChannelModels{ChannelId: channelID}).Data(do.ChannelModels{
			HealthScore:        system.ModelHealthInitialScore,
			AutoDisabledAt:     gdb.Raw("NULL"),
			AutoDisabledReason: gdb.Raw("NULL"),
			AutoDisabledSource: gdb.Raw("NULL"),
		}).Update(); err != nil {
			return gerror.Wrap(err, "recover channel models")
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
	if err = s.invalidateRoutes(ctx); err != nil {
		return err
	}
	// 手动开关渠道是管理员的显式动作，需要留痕。启用渠道会连带恢复全部密钥，
	// 但那是「打开渠道」的连带效果，不逐把发密钥邮件，避免一次点开关收几十封。
	if previous.Status != status {
		s.resilience.NotifyChannelStatusChange(ctx, channelID, status == 1)
	}
	return nil
}
