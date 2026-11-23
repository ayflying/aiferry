package mail

import (
	"context"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/logic/system"
)

// 相同对象的同方向告警在 15 分钟内最多发送一封。恢复和故障各自限频，
// 避免一次恢复挡住首封故障，也不让频繁来回切换反复发信。
const availabilityMailInterval = 15 * time.Minute

func availabilityMailKey(notification system.AutoDisableNotification) string {
	if notification.Manual {
		return ""
	}
	switch notification.Kind {
	case system.NotificationKindModelAvailability:
		return fmt.Sprintf("aiferry:mail:availability:model:%d:%d:%t", notification.ChannelID, notification.ModelID, notification.Recovered)
	case system.NotificationKindChannelAvailability:
		return fmt.Sprintf("aiferry:mail:availability:channel:%d:%t", notification.ChannelID, notification.Recovered)
	default:
		return ""
	}
}

func (s *sMail) allowAvailabilityMail(ctx context.Context, notification system.AutoDisableNotification) bool {
	key := availabilityMailKey(notification)
	if key == "" {
		return true
	}
	if s.app == nil || s.app.Redis == nil {
		return true
	}
	// 原子占用发送窗口，多进程同时处理也只有一个能发；只影响邮件，不改变路由状态。
	created, err := s.app.Redis.SetNX(ctx, key, "1", availabilityMailInterval).Result()
	if err != nil {
		g.Log().Warningf(ctx, "reserve availability mail window: %v", err)
		return true // 缓存故障时保留告警，避免因此丢掉真实故障通知。
	}
	return created
}
