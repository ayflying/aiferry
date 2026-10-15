package channel

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// comboRecoveryCandidate 是健康分归零（组合已隔离）的「模型 × 密钥」组合。
type comboRecoveryCandidate struct {
	Id                  uint64      `orm:"id"`
	ChannelId           uint64      `orm:"channel_id"`
	ChannelModelId      uint64      `orm:"channel_model_id"`
	ChannelCredentialId uint64      `orm:"channel_credential_id"`
	HealthScore         int         `orm:"health_score"`
	CooldownUntil       *gtime.Time `orm:"cooldown_until"`
}

// comboRecoveryDue 判断某个组合本轮是否该被恢复巡检重测：健康分必须已经归零
// （组合被隔离），且冷却已到期。冷却未到期的一律跳过——5 分钟瞬时限流与
// 「免费档日配额耗尽（冷却到 UTC 零点）」都靠这段时间生效，提前重测会像历史事故
// 那样反复撞限流，把恢复巡检变成死循环。
func comboRecoveryDue(healthScore int, cooldownUntil, now *gtime.Time) bool {
	if healthScore > system.ModelHealthDisableScore {
		return false
	}
	if cooldownUntil == nil {
		return true
	}
	return cooldownUntil.TimestampMilli() <= now.TimestampMilli()
}

// runComboRecoveryChecks 定期重测被隔离的「模型 × 密钥」组合，成功即加分解除隔离。
//
// 组合健康评分（2026-09-17）把模型维度的禁用从模型级标记搬到了组合级隔离：
// channel_models.auto_disabled_at 不再被写入，只按模型级标记扫描的
// runModelRecoveryChecks 因此永远扫不到行，自动测试与解禁模型实际空转。
// 这里按组合口径补齐。冷却时间本身就是节流：测试失败会把健康分重新归零并刷新冷却，
// 测试成功则由 TestModel 加 5 分并清空冷却（见 BumpComboHealthScore 与
// changeComboHealth）。
func (s *sChannel) runComboRecoveryChecks(ctx context.Context) {
	candidates, err := s.loadComboRecoveryCandidates(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "load combo recovery candidates: %v", err)
		return
	}
	for _, candidate := range candidates {
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{
			ModelID:             candidate.ChannelModelId,
			ChannelCredentialID: candidate.ChannelCredentialId,
			Endpoint:            "auto",
		}, usage.SystemUserID)
		cancel()
	}
}

// loadComboRecoveryCandidates 选出本轮该重测的组合：组合健康分归零且冷却已到期，
// 所属模型仍启用、渠道仍在巡检范围（启用中，或因模型全部被禁用而自动关闭的渠道）、
// 密钥仍有效。被禁用的密钥交给密钥级恢复路径，组合巡检不再碰它，否则会在同一把
// 失效密钥上反复测试。
func (s *sChannel) loadComboRecoveryCandidates(ctx context.Context) ([]comboRecoveryCandidate, error) {
	columns := dao.ChannelModelCredentials.Columns()
	rows := make([]comboRecoveryCandidate, 0)
	if err := dao.ChannelModelCredentials.Ctx(ctx).
		Fields(columns.Id, columns.ChannelId, columns.ChannelModelId, columns.ChannelCredentialId, columns.HealthScore, columns.CooldownUntil).
		Where(columns.HealthScore, system.ModelHealthDisableScore).
		OrderAsc(columns.Id).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "load isolated model credential combos")
	}
	now := gtime.Now()
	due := make([]comboRecoveryCandidate, 0, len(rows))
	channelIDSet := make(map[uint64]struct{}, len(rows))
	modelIDSet := make(map[uint64]struct{}, len(rows))
	credentialIDSet := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		if !comboRecoveryDue(row.HealthScore, row.CooldownUntil, now) {
			continue
		}
		due = append(due, row)
		channelIDSet[row.ChannelId] = struct{}{}
		modelIDSet[row.ChannelModelId] = struct{}{}
		credentialIDSet[row.ChannelCredentialId] = struct{}{}
	}
	if len(due) == 0 {
		return nil, nil
	}

	modelColumns := dao.ChannelModels.Columns()
	models := make([]entity.ChannelModels, 0)
	if err := dao.ChannelModels.Ctx(ctx).
		Fields(modelColumns.Id).
		WhereIn(modelColumns.Id, sortedModelIDs(modelIDSet)).
		Where(modelColumns.Enabled, 1).
		Scan(&models); err != nil {
		return nil, gerror.Wrap(err, "load combo recovery models")
	}
	enabledModels := make(map[uint64]struct{}, len(models))
	for _, model := range models {
		enabledModels[model.Id] = struct{}{}
	}

	channels, err := loadModelRecoveryChannels(ctx, sortedModelIDs(channelIDSet))
	if err != nil {
		return nil, gerror.Wrap(err, "load combo recovery channels")
	}
	activeChannels := make(map[uint64]struct{}, len(channels))
	for _, channel := range channels {
		activeChannels[channel.Id] = struct{}{}
	}

	credentialColumns := dao.ChannelCredentials.Columns()
	credentials := make([]entity.ChannelCredentials, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).
		Fields(credentialColumns.Id).
		WhereIn(credentialColumns.Id, sortedModelIDs(credentialIDSet)).
		Where(credentialColumns.Status, 1).
		Scan(&credentials); err != nil {
		return nil, gerror.Wrap(err, "load combo recovery credentials")
	}
	activeCredentials := make(map[uint64]struct{}, len(credentials))
	for _, credential := range credentials {
		activeCredentials[credential.Id] = struct{}{}
	}

	candidates := make([]comboRecoveryCandidate, 0, len(due))
	for _, row := range due {
		if _, ok := enabledModels[row.ChannelModelId]; !ok {
			continue
		}
		if _, ok := activeChannels[row.ChannelId]; !ok {
			continue
		}
		if _, ok := activeCredentials[row.ChannelCredentialId]; !ok {
			continue
		}
		candidates = append(candidates, row)
	}
	return candidates, nil
}
