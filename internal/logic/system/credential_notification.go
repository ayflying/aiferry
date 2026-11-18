package system

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
)

// 密钥维度的通知口径：一把密钥就是一个独立的上游账号。
//
// 生产上为了少建渠道，会在同一个渠道里配置多把密钥（不同账号、不同额度）。因此
// 任何一把密钥被禁用或恢复，都等价于「少了一个可用渠道 / 多回来一个」，必须逐把
// 单独告知；按渠道做「全部密钥都不可用」的汇总会把这个信息吞掉。

// CredentialStatusNotification 描述单把密钥的一次状态变化。
type CredentialStatusNotification struct {
	ChannelID           uint64
	CredentialID        uint64
	CredentialKeyPrefix string
	// Recovered 表示本次是恢复方向。
	Recovered bool
	// Manual 表示变化来自管理端手动操作。
	Manual     bool
	Source     string
	StatusCode uint
	Reason     string
}

// NotifyCredentialStatusChange 在单把密钥被禁用或恢复后发信。
// 自动禁用、自动恢复、手动启停都走这里，保证同一把密钥的三种来源措辞一致。
func (s *sSystem) NotifyCredentialStatusChange(ctx context.Context, notification CredentialStatusNotification) {
	settings, err := s.Get(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "load settings for credential notification: %v", err)
		return
	}
	s.notifyAutoDisableTransition(ctx, settings, AutoDisableNotification{
		Kind:                NotificationKindCredentialStatus,
		Recovered:           notification.Recovered,
		Manual:              notification.Manual,
		ChannelID:           notification.ChannelID,
		ChannelName:         s.autoDisableNotificationChannelName(ctx, notification.ChannelID),
		CredentialID:        notification.CredentialID,
		CredentialKeyPrefix: notification.CredentialKeyPrefix,
		Source:              notification.Source,
		StatusCode:          notification.StatusCode,
		Reason:              notification.Reason,
	})
}
