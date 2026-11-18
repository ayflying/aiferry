package system

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 渠道的「密钥可用性」口径：渠道下至少还有一个启用的密钥，才算这个渠道还能承接请求。
// 该口径只用于决定要不要发邮件，不参与渠道 Status 的读写：渠道本身可能一直是启用状态，
// 却因为密钥全部被禁用而实际不可用（渠道列表里的 CredentialsUnavailable 用的是同一口径）。

// CredentialAvailability 描述一次密钥启停前后，渠道可用性的变化方向。
type CredentialAvailability int

const (
	// CredentialAvailabilityUnchanged 表示可用密钥数变化但没跨过 0 的边界，
	// 例如 3→2 仍然可用、0→0 仍然不可用，都不需要打扰管理员。
	CredentialAvailabilityUnchanged CredentialAvailability = iota
	// CredentialAvailabilityLost 表示渠道从「有可用密钥」变成「没有可用密钥」。
	CredentialAvailabilityLost
	// CredentialAvailabilityRestored 表示渠道从「没有可用密钥」变成「有可用密钥」。
	CredentialAvailabilityRestored
)

// CredentialAvailabilityChange 描述本次密钥启停的上下文。
type CredentialAvailabilityChange struct {
	// Before 是操作前该渠道启用的密钥数量，必须由调用方在写库之前查询。
	Before int
	// 以下几项只用于邮件正文说明，手动启停时可以留空。
	CredentialID        uint64
	CredentialKeyPrefix string
	Source              string
	StatusCode          uint
	Reason              string
}

// credentialAvailabilityTransition 由前后可用密钥数推断渠道可用性方向。
// 只有跨越 0 的边界才算变化：中间状态不发信，避免每掉一个密钥就发一封。
func credentialAvailabilityTransition(before, after int) CredentialAvailability {
	switch {
	case before > 0 && after == 0:
		return CredentialAvailabilityLost
	case before == 0 && after > 0:
		return CredentialAvailabilityRestored
	default:
		return CredentialAvailabilityUnchanged
	}
}

// CountActiveCredentials 返回渠道下当前启用的密钥数量。
func (s *sSystem) CountActiveCredentials(ctx context.Context, channelID uint64) (int, error) {
	if channelID == 0 {
		return 0, nil
	}
	count, err := dao.ChannelCredentials.Ctx(ctx).
		Where(do.ChannelCredentials{ChannelId: channelID, Status: 1}).Count()
	if err != nil {
		return 0, gerror.Wrap(err, "count active channel credentials")
	}
	return count, nil
}

// NotifyCredentialAvailabilityChange 在密钥启停写库之后调用：只有跨过「0 个可用密钥」
// 这个边界才发邮件——全部密钥都不可用时提醒一次，恢复任意一个时再提醒一次。
//
// 单密钥的禁用与恢复不再单独发信：一条渠道挂三个密钥时，逐条发信只会变成噪音，
// 管理员真正需要知道的是「这个渠道还能不能用」。
func (s *sSystem) NotifyCredentialAvailabilityChange(ctx context.Context, channelID uint64, change CredentialAvailabilityChange) {
	after, err := s.CountActiveCredentials(ctx, channelID)
	if err != nil {
		g.Log().Warningf(ctx, "count active credentials for channel %d: %v", channelID, err)
		return
	}
	direction := credentialAvailabilityTransition(change.Before, after)
	if direction == CredentialAvailabilityUnchanged {
		return
	}
	settings, err := s.Get(ctx)
	if err != nil {
		return
	}
	s.notifyAutoDisableTransition(ctx, settings, AutoDisableNotification{
		Kind:                  NotificationKindCredentialAvailability,
		Recovered:             direction == CredentialAvailabilityRestored,
		ActiveCredentialCount: after,
		ChannelID:             channelID,
		ChannelName:           s.autoDisableNotificationChannelName(ctx, channelID),
		CredentialID:          change.CredentialID,
		CredentialKeyPrefix:   change.CredentialKeyPrefix,
		Reason:                change.Reason,
		Source:                change.Source,
		StatusCode:            change.StatusCode,
	})
}

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
// 组合口径（2026-09-17 起模型级禁用被组合隔离取代）：某个模型在某把密钥上失败只隔离
// 这一个「模型 × 密钥」组合，不牵连同渠道的其它密钥。因此只要还有一把启用密钥没有
// 在冷却中，这个模型就仍然可用；还没建立组合行的密钥按满分处理，路由会正常选中它。
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
