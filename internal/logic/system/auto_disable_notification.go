package system

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// NotificationKind 区分通知的语义，邮件渲染按它选择措辞与标题。
type NotificationKind int

const (
	// NotificationKindAutoDisable 沿用原有语义：按 CredentialID / ModelID 判断是
	// 渠道、密钥还是模型被自动禁用或自动恢复。
	NotificationKindAutoDisable NotificationKind = iota
	// NotificationKindCredentialStatus 表示单把上游密钥被禁用或恢复。
	// 生产上同一渠道会放多把密钥（每把是一个独立账号），这类通知逐把发送。
	NotificationKindCredentialStatus
	// NotificationKindModelAvailability 表示单个模型在该渠道暂时没有可用组合，或恢复。
	NotificationKindModelAvailability
	// NotificationKindChannelAvailability 表示整个渠道不可用或恢复：手动开关渠道，
	// 或渠道内已经没有任何可用模型。
	NotificationKindChannelAvailability
)

// AutoDisableNotification describes a completed automatic channel state transition.
type AutoDisableNotification struct {
	Kind                NotificationKind
	Recovered           bool
	ChannelID           uint64
	ChannelName         string
	CredentialID        uint64
	CredentialKeyPrefix string
	ModelID             uint64
	ModelName           string
	Reason              string
	Source              string
	StatusCode          uint
	// Manual 表示本次变化来自管理端手动操作，而不是自动探测。
	Manual bool
	// AvailableModelCount 是变化之后渠道仍有可用组合的模型数量。
	AvailableModelCount int
}

// AutoDisableNotifier keeps channel state management independent from delivery details.
type AutoDisableNotifier interface {
	NotifyAutoDisableTransition(ctx context.Context, notification AutoDisableNotification)
}

func SetAutoDisableNotifier(s *sSystem, notifier AutoDisableNotifier) {
	s.autoDisableNotifier = notifier
}

func (s *sSystem) notifyAutoDisableTransition(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, notification AutoDisableNotification) {
	if !settings.AutoDisableNotificationEnabled || s.autoDisableNotifier == nil {
		return
	}
	s.autoDisableNotifier.NotifyAutoDisableTransition(ctx, notification)
}

func (s *sSystem) autoDisableNotificationChannelName(ctx context.Context, channelID uint64) string {
	var channel entity.Channels
	if err := dao.Channels.Ctx(ctx).Fields(dao.Channels.Columns().Name).Where(do.Channels{Id: channelID}).Scan(&channel); err != nil {
		return ""
	}
	return channel.Name
}

func notificationStatusCode(status int) uint {
	if status <= 0 {
		return 0
	}
	return uint(status)
}
