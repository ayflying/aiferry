package system

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
)

// 模型维度的可用性通知。
//
// 2026-09-17 起模型级自动禁用被「模型 × 密钥」组合隔离取代：某把密钥上的失败只隔离
// 这一个组合，不会把整个模型停掉（否则一把坏密钥会拖垮同渠道里所有模型）。
// 因此「模型不可用」的判定改成：该模型在当前渠道下所有启用密钥的组合都在冷却中。
//
// 发信口径与中转渠道的实际形态对齐：一个模型挂掉时只发模型级邮件（渠道里其它模型
// 照常工作），只有整个渠道一个可用模型都不剩时才升级为渠道级邮件。

// NotifyModelAvailabilityTransition 在某个模型的组合隔离/恢复之后调用。
// recovered 表示本次是恢复方向；函数内部按当前真实状态判断该发模型级还是渠道级。
func (s *sSystem) NotifyModelAvailabilityTransition(ctx context.Context, channelID, modelID uint64, recovered bool) {
	items, err := s.loadModelAvailability(ctx, channelID)
	if err != nil {
		g.Log().Warningf(ctx, "load model availability for channel %d: %v", channelID, err)
		return
	}
	target := modelAvailability{ModelID: modelID}
	for _, item := range items {
		if item.ModelID == modelID {
			target = item
			break
		}
	}
	available := countAvailableModels(items)
	notification := AutoDisableNotification{
		ChannelID:           channelID,
		ChannelName:         s.autoDisableNotificationChannelName(ctx, channelID),
		ModelID:             modelID,
		ModelName:           target.Name,
		AvailableModelCount: available,
	}
	switch {
	case recovered && !target.Available:
		// 组合恢复了但这把密钥不是模型仅剩的可用组合，模型整体仍不可用，不发。
		return
	case recovered && available == 1:
		// 刚恢复的模型就是当前唯一可用模型，说明恢复前整个渠道都没有可用模型。
		notification.Kind = NotificationKindChannelAvailability
		notification.Recovered = true
	case recovered:
		notification.Kind = NotificationKindModelAvailability
		notification.Recovered = true
	case target.Available:
		// 还有其它启用密钥没被隔离，模型没变成不可用，不发。
		return
	case available == 0:
		notification.Kind = NotificationKindChannelAvailability
	default:
		notification.Kind = NotificationKindModelAvailability
	}
	settings, err := s.Get(ctx)
	if err != nil {
		return
	}
	s.notifyAutoDisableTransition(ctx, settings, notification)
}

// NotifyChannelStatusChange 在管理端手动启用/禁用渠道之后调用。
// 手动关渠道意味着整个渠道退出路由，属于管理员需要留痕的动作，因此单独发信。
func (s *sSystem) NotifyChannelStatusChange(ctx context.Context, channelID uint64, enabled bool) {
	settings, err := s.Get(ctx)
	if err != nil {
		return
	}
	s.notifyAutoDisableTransition(ctx, settings, AutoDisableNotification{
		Kind:        NotificationKindChannelAvailability,
		Recovered:   enabled,
		Manual:      true,
		ChannelID:   channelID,
		ChannelName: s.autoDisableNotificationChannelName(ctx, channelID),
	})
}
