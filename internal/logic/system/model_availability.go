package system

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 模型维度的可用性判定。
//
// 2026-09-17 起模型级自动禁用被「模型 × 密钥」组合隔离取代：某把密钥上的失败只隔离
// 这一个组合，不会把整个模型停掉（否则一把坏密钥会拖垮同渠道里所有模型）。
// 因此「模型不可用」的判定是：该模型在当前渠道下所有启用密钥的组合都在冷却中。
//
// 注意密钥维度不在这里汇总：同一渠道里放多把密钥只是为了减少配置，每把密钥实际
// 代表一个独立的上游账号，谁挂谁恢复都要单独告知（见 credential_notification.go）。

// modelAvailability 描述渠道内单个模型当前是否还有可用的「模型 × 密钥」组合。
type modelAvailability struct {
	ModelID   uint64
	Name      string
	Enabled   bool
	Available bool
}

// availabilityNow 是判定组合冷却是否已过期的基准时间，抽成函数便于单测固定时钟。
func availabilityNow() *gtime.Time {
	return gtime.Now()
}

// comboCooldownActive 判断组合冷却是否仍在生效；没有冷却时间表示未被隔离。
func comboCooldownActive(cooldown, now *gtime.Time) bool {
	if cooldown == nil {
		return false
	}
	return cooldown.TimestampMilli() > now.TimestampMilli()
}

// modelComboAvailable 判断模型在给定启用密钥集合下是否还有可用组合。
//
// 只要还有一把启用密钥没有在冷却中，这个模型就仍然可用；还没建立组合行的密钥按
// 满分处理，路由会正常选中它。
func modelComboAvailable(modelID uint64, activeCredentialIDs map[uint64]struct{}, cooldowns map[uint64]map[uint64]*gtime.Time, now *gtime.Time) bool {
	if len(activeCredentialIDs) == 0 {
		return false
	}
	byCredential := cooldowns[modelID]
	for credentialID := range activeCredentialIDs {
		cooldown, exists := byCredential[credentialID]
		if !exists {
			return true
		}
		if !comboCooldownActive(cooldown, now) {
			return true
		}
	}
	return false
}

// loadModelAvailability 汇总渠道内每个模型的组合可用性。
// 只有启用的密钥算数：被禁用的密钥本来就不参与路由，挂在它上面的组合是否冷却
// 不影响模型能不能用。
func (s *sSystem) loadModelAvailability(ctx context.Context, channelID uint64) ([]modelAvailability, error) {
	models := make([]entity.ChannelModels, 0)
	if err := dao.ChannelModels.Ctx(ctx).
		Where(do.ChannelModels{ChannelId: channelID}).
		OrderAsc(dao.ChannelModels.Columns().Id).
		Scan(&models); err != nil {
		return nil, gerror.Wrap(err, "load channel models for availability")
	}
	if len(models) == 0 {
		return nil, nil
	}
	credentials := make([]entity.ChannelCredentials, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).
		Where(do.ChannelCredentials{ChannelId: channelID, Status: 1}).
		Scan(&credentials); err != nil {
		return nil, gerror.Wrap(err, "load active credentials for availability")
	}
	activeCredentialIDs := make(map[uint64]struct{}, len(credentials))
	for _, credential := range credentials {
		activeCredentialIDs[credential.Id] = struct{}{}
	}
	combos := make([]entity.ChannelModelCredentials, 0)
	if err := dao.ChannelModelCredentials.Ctx(ctx).
		Where(do.ChannelModelCredentials{ChannelId: channelID}).
		Scan(&combos); err != nil {
		return nil, gerror.Wrap(err, "load model credential combos for availability")
	}
	cooldowns := make(map[uint64]map[uint64]*gtime.Time, len(combos))
	for _, combo := range combos {
		if _, ok := activeCredentialIDs[combo.ChannelCredentialId]; !ok {
			continue
		}
		if cooldowns[combo.ChannelModelId] == nil {
			cooldowns[combo.ChannelModelId] = make(map[uint64]*gtime.Time, 4)
		}
		cooldowns[combo.ChannelModelId][combo.ChannelCredentialId] = combo.CooldownUntil
	}
	now := availabilityNow()
	result := make([]modelAvailability, 0, len(models))
	for _, model := range models {
		item := modelAvailability{ModelID: model.Id, Name: model.PublicName, Enabled: model.Enabled == 1}
		item.Available = item.Enabled && modelComboAvailable(model.Id, activeCredentialIDs, cooldowns, now)
		result = append(result, item)
	}
	return result, nil
}

// countAvailableModels 返回渠道内真正还有可用组合的启用模型数量。
func countAvailableModels(items []modelAvailability) int {
	count := 0
	for _, item := range items {
		if item.Available {
			count++
		}
	}
	return count
}
