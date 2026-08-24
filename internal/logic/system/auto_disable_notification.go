package system

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// AutoDisableNotification describes a completed automatic channel state transition.
type AutoDisableNotification struct {
	Recovered           bool
	ChannelID           uint64
	ChannelName         string
	CredentialID        uint64
	CredentialKeyPrefix string
	Reason              string
	Source              string
	StatusCode          uint
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
