package channel

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const healthCheckTick = 10 * time.Second

func (s *sChannel) StartHealthChecks(ctx context.Context) {
	go func() {
		lastHealthCheck := time.Now()
		ticker := time.NewTicker(healthCheckTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				settings, err := s.resilience.Get(ctx)
				if err != nil {
					continue
				}
				interval := time.Duration(settings.HealthCheckIntervalMinutes) * time.Minute
				if !healthCheckDue(now, lastHealthCheck, interval) {
					continue
				}
				lastHealthCheck = now
				recovery, regular := healthActions(settings)
				if recovery {
					s.runRecoveryChecks(ctx, settings.HealthCheckMode)
				}
				if regular {
					s.runRegularHealthChecks(ctx, settings.HealthCheckMode)
				}
			}
		}
	}()
}

func healthCheckDue(now, last time.Time, interval time.Duration) bool {
	return interval > 0 && !now.Before(last.Add(interval))
}

// healthActions 判定本轮调度应执行哪些检查。恢复检查（测试被禁目标并解禁）
// 只受恢复开关控制；主动巡检正常渠道才受健康检查开关控制——历史上恢复检查
// 被误挂在健康检查开关下，导致默认配置下被禁模型永不自动解禁。
func healthActions(settings adminapi.SystemResilienceSettingsInput) (recovery bool, regular bool) {
	return settings.RecoveryEnabled, settings.HealthCheckEnabled
}

func (s *sChannel) runRegularHealthChecks(ctx context.Context, mode string) {
	if mode != "all" {
		return
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0)
	if err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.HealthCheckModelId).
		Where(columns.Status, 1).
		Where(columns.AutoDisableEnabled, 1).
		OrderAsc(columns.Id).
		Scan(&channels); err != nil {
		g.Log().Warningf(ctx, "load regular channel health checks: %v", err)
		return
	}
	modelIDs, err := loadHealthCheckModelIDs(ctx, channels)
	if err != nil {
		g.Log().Warningf(ctx, "load regular health check models: %v", err)
		return
	}
	for _, channel := range channels {
		modelID := modelIDs[channel.Id]
		if modelID == 0 {
			continue
		}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: modelID, Endpoint: "auto"}, usage.SystemUserID)
		cancel()
	}
}

func (s *sChannel) runRecoveryChecks(ctx context.Context, mode string) {
	s.runChannelRecoveryChecks(ctx, mode)
	s.runCredentialRecoveryChecks(ctx, mode)
	s.runModelRecoveryChecks(ctx, mode)
}

func (s *sChannel) runChannelRecoveryChecks(ctx context.Context, mode string) {
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0)
	model := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.AutoDisabledAt, columns.HealthCheckModelId).
		Where(columns.Status, 0).
		Where(columns.AutoDisableEnabled, 1).
		WhereNotNull(columns.AutoDisabledAt).
		OrderAsc(columns.Id)
	if mode == "passive" {
		model = model.Where(columns.AutoDisabledSource, system.AutoDisableSourceRelayRequest)
	}
	if err := model.Scan(&channels); err != nil {
		g.Log().Warningf(ctx, "load channel recovery checks: %v", err)
		return
	}
	modelIDs, err := loadHealthCheckModelIDs(ctx, channels)
	if err != nil {
		g.Log().Warningf(ctx, "load channel recovery models: %v", err)
		return
	}
	for _, channel := range channels {
		modelID := modelIDs[channel.Id]
		if modelID == 0 {
			continue
		}
		started, err := s.resilience.BeginRecoveryAttempt(ctx, system.RecoveryTargetChannel, channel.Id, channel.AutoDisabledAt)
		if err != nil {
			g.Log().Warningf(ctx, "schedule channel %d recovery: %v", channel.Id, err)
			continue
		}
		if !started {
			continue
		}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, testErr := s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: modelID, Endpoint: "auto"}, usage.SystemUserID)
		cancel()
		s.resilience.FinishRecoveryAttempt(ctx, system.RecoveryTargetChannel, channel.Id, testErr == nil && result.Success)
	}
}

func (s *sChannel) runCredentialRecoveryChecks(ctx context.Context, mode string) {
	credentialColumns := dao.ChannelCredentials.Columns()
	credentials := make([]entity.ChannelCredentials, 0)
	model := dao.ChannelCredentials.Ctx(ctx).
		Fields(credentialColumns.Id, credentialColumns.ChannelId, credentialColumns.AutoDisabledAt).
		Where(credentialColumns.Status, 0).
		WhereNotNull(credentialColumns.AutoDisabledAt).
		OrderAsc(credentialColumns.Id)
	if mode == "passive" {
		model = model.Where(credentialColumns.AutoDisabledSource, system.AutoDisableSourceRelayRequest)
	}
	if err := model.Scan(&credentials); err != nil {
		g.Log().Warningf(ctx, "load credential recovery checks: %v", err)
		return
	}
	channels, err := loadActiveHealthChannels(ctx, healthCredentialChannelIDs(credentials))
	if err != nil {
		g.Log().Warningf(ctx, "load credential recovery channels: %v", err)
		return
	}
	modelIDs, err := loadHealthCheckModelIDs(ctx, channels)
	if err != nil {
		g.Log().Warningf(ctx, "load credential recovery models: %v", err)
		return
	}
	for _, credential := range credentials {
		modelID := modelIDs[credential.ChannelId]
		if modelID == 0 {
			continue
		}
		started, err := s.resilience.BeginRecoveryAttempt(ctx, system.RecoveryTargetCredential, credential.Id, credential.AutoDisabledAt)
		if err != nil {
			g.Log().Warningf(ctx, "schedule credential %d recovery: %v", credential.Id, err)
			continue
		}
		if !started {
			continue
		}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, testErr := s.TestModel(testCtx, adminapi.ModelTestInput{
			ModelID: modelID, ChannelCredentialID: credential.Id, Endpoint: "auto",
		}, usage.SystemUserID)
		cancel()
		s.resilience.FinishRecoveryAttempt(ctx, system.RecoveryTargetCredential, credential.Id, testErr == nil && result.Success)
	}
}

// runModelRecoveryChecks 定期测试被自动禁用的模型；测试成功会清除模型禁用标记
// 并重置健康评分（见 TestModel 内的 RecoverModelIfAllowed）。
func (s *sChannel) runModelRecoveryChecks(ctx context.Context, mode string) {
	columns := dao.ChannelModels.Columns()
	models := make([]entity.ChannelModels, 0)
	model := dao.ChannelModels.Ctx(ctx).
		Fields(columns.Id, columns.ChannelId, columns.AutoDisabledAt).
		Where(columns.Enabled, 1).
		WhereNotNull(columns.AutoDisabledAt).
		OrderAsc(columns.Id)
	if mode == "passive" {
		model = model.Where(columns.AutoDisabledSource, system.AutoDisableSourceRelayRequest)
	}
	if err := model.Scan(&models); err != nil {
		g.Log().Warningf(ctx, "load model recovery checks: %v", err)
		return
	}
	if len(models) == 0 {
		return
	}
	channelIDs := make(map[uint64]struct{}, len(models))
	for _, item := range models {
		channelIDs[item.ChannelId] = struct{}{}
	}
	channels, err := loadModelRecoveryChannels(ctx, sortedModelIDs(channelIDs))
	if err != nil {
		g.Log().Warningf(ctx, "load model recovery channels: %v", err)
		return
	}
	activeChannels := make(map[uint64]struct{}, len(channels))
	for _, channel := range channels {
		activeChannels[channel.Id] = struct{}{}
	}
	for _, item := range models {
		if _, active := activeChannels[item.ChannelId]; !active {
			continue
		}
		started, err := s.resilience.BeginRecoveryAttempt(ctx, system.RecoveryTargetModel, item.Id, *item.AutoDisabledAt)
		if err != nil {
			g.Log().Warningf(ctx, "schedule model %d recovery: %v", item.Id, err)
			continue
		}
		if !started {
			continue
		}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, testErr := s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: item.Id, Endpoint: "auto"}, usage.SystemUserID)
		cancel()
		s.resilience.FinishRecoveryAttempt(ctx, system.RecoveryTargetModel, item.Id, testErr == nil && result.Success)
	}
}

func loadHealthCheckModelIDs(ctx context.Context, channels []entity.Channels) (map[uint64]uint64, error) {	result := make(map[uint64]uint64)
	if len(channels) == 0 {
		return result, nil
	}
	channelIDs := make(map[uint64]struct{}, len(channels))
	for _, channel := range channels {
		channelIDs[channel.Id] = struct{}{}
	}
	modelColumns := dao.ChannelModels.Columns()
	models := make([]entity.ChannelModels, 0)
	if err := dao.ChannelModels.Ctx(ctx).
		Fields(modelColumns.Id, modelColumns.ChannelId).
		WhereIn(modelColumns.ChannelId, sortedModelIDs(channelIDs)).
		Where(modelColumns.Enabled, 1).
		OrderAsc(modelColumns.Id).
		Scan(&models); err != nil {
		return nil, err
	}
	modelsByChannel := make(map[uint64][]entity.ChannelModels)
	for _, model := range models {
		modelsByChannel[model.ChannelId] = append(modelsByChannel[model.ChannelId], model)
	}
	for _, channel := range channels {
		if modelID := selectHealthCheckModelID(channel.HealthCheckModelId, modelsByChannel[channel.Id]); modelID > 0 {
			result[channel.Id] = modelID
		}
	}
	return result, nil
}

func loadActiveHealthChannels(ctx context.Context, channelIDs []uint64) ([]entity.Channels, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0, len(channelIDs))
	err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.HealthCheckModelId).
		WhereIn(columns.Id, channelIDs).
		Where(columns.Status, 1).
		Where(columns.AutoDisableEnabled, 1).
		OrderAsc(columns.Id).
		Scan(&channels)
	return channels, err
}

// loadModelRecoveryChannels 加载模型恢复检查可探测的渠道。除启用中的渠道外，
// 还包含「因所有模型被扣分禁用而被自动关闭」的渠道（Status=0 且 AutoDisabledAt
// 非空）——否则模型恢复检查跳过这些渠道，渠道又被被动模式的来源过滤卡死，
// 形成两边都够不着的死锁。手动关闭的渠道 AutoDisabledAt 为空，仍被排除。
func loadModelRecoveryChannels(ctx context.Context, channelIDs []uint64) ([]entity.Channels, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0, len(channelIDs))
	err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.HealthCheckModelId).
		WhereIn(columns.Id, channelIDs).
		Where(columns.AutoDisableEnabled, 1).
		Where("(" + columns.Status + " = 1) OR (" + columns.Status + " = 0 AND " + columns.AutoDisabledAt + " IS NOT NULL)").
		OrderAsc(columns.Id).
		Scan(&channels)
	return channels, err
}

func healthCredentialChannelIDs(credentials []entity.ChannelCredentials) []uint64 {
	ids := make(map[uint64]struct{}, len(credentials))
	for _, credential := range credentials {
		ids[credential.ChannelId] = struct{}{}
	}
	return sortedModelIDs(ids)
}
