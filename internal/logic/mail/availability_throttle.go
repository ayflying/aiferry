package mail

import (
	"context"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/logic/system"
)

const (
	// 同一对象（模型 / 密钥 / 渠道）的同方向自动告警最多每 15 分钟一封。
	// 故障与恢复各自拥有独立窗口：一次恢复不该占掉后续首封故障的配额，
	// 但同一方向反复来回也不会连续发信。
	availabilityMailInterval = 15 * time.Minute
	// 单个渠道每小时最多 4 封自动告警。多模型共用同一上游时，单个模型限频
	// 挡不住「每个模型各发一封」的放大效应，渠道级预算给出总量上限。
	availabilityChannelBudget = 4
	availabilityChannelWindow = time.Hour
)

// availabilityMailDecision 是一封自动可用性邮件的放行结论。
// Reason 只用于日志，便于线上确认邮件究竟是发出还是被哪条限频规则拦下。
type availabilityMailDecision struct {
	Allow  bool
	Reason string
}

// availabilityMailKey 返回限频键：同键在 availabilityMailInterval 内只放行一封。
// 手动操作返回空键，表示不参与限频（管理端动作必须留痕）。
func availabilityMailKey(notification system.AutoDisableNotification) string {
	if notification.Manual {
		return ""
	}
	switch notification.Kind {
	case system.NotificationKindModelAvailability:
		return fmt.Sprintf("aiferry:mail:availability:model:%d:%d:%t", notification.ChannelID, notification.ModelID, notification.Recovered)
	case system.NotificationKindCredentialStatus:
		return fmt.Sprintf("aiferry:mail:availability:credential:%d:%d:%t", notification.ChannelID, notification.CredentialID, notification.Recovered)
	case system.NotificationKindChannelAvailability:
		return fmt.Sprintf("aiferry:mail:availability:channel:%d:%t", notification.ChannelID, notification.Recovered)
	default:
		// 旧口径通知（Kind 零值）按已有细粒度字段退化为密钥级或模型级键，
		// 避免这类事件绕过限频直接发信。
		if notification.CredentialID > 0 {
			return fmt.Sprintf("aiferry:mail:availability:credential:%d:%d:%t", notification.ChannelID, notification.CredentialID, notification.Recovered)
		}
		if notification.ModelID > 0 {
			return fmt.Sprintf("aiferry:mail:availability:model:%d:%d:%t", notification.ChannelID, notification.ModelID, notification.Recovered)
		}
		return fmt.Sprintf("aiferry:mail:availability:channel:%d:%t", notification.ChannelID, notification.Recovered)
	}
}

// allowAvailabilityMail 依次检查对象级窗口与渠道级预算。
// 缓存异常时一律放行：宁可多发一封，也不要因为 Redis 抖动漏掉真实故障。
func (s *sMail) allowAvailabilityMail(ctx context.Context, notification system.AutoDisableNotification) availabilityMailDecision {
	if notification.Manual {
		return availabilityMailDecision{Allow: true, Reason: "manual"}
	}
	key := availabilityMailKey(notification)
	if key == "" {
		return availabilityMailDecision{Allow: true, Reason: "no-key"}
	}
	if s.app == nil || s.app.Redis == nil {
		return availabilityMailDecision{Allow: true, Reason: "no-cache"}
	}
	created, err := s.app.Redis.SetNX(ctx, key, "1", availabilityMailInterval).Result()
	if err != nil {
		g.Log().Warningf(ctx, "reserve availability mail window: %v", err)
		return availabilityMailDecision{Allow: true, Reason: "cache-error"}
	}
	if !created {
		return availabilityMailDecision{Reason: "object-window"}
	}
	if notification.ChannelID == 0 {
		return availabilityMailDecision{Allow: true}
	}
	budgetKey := fmt.Sprintf("aiferry:mail:availability:budget:%d", notification.ChannelID)
	used, err := s.app.Redis.Incr(ctx, budgetKey).Result()
	if err != nil {
		g.Log().Warningf(ctx, "reserve availability mail budget: %v", err)
		return availabilityMailDecision{Allow: true, Reason: "cache-error"}
	}
	if used == 1 {
		// 首次计数时设窗口；后续自增沿用同一过期时间。
		_ = s.app.Redis.Expire(ctx, budgetKey, availabilityChannelWindow).Err()
	}
	if used > availabilityChannelBudget {
		return availabilityMailDecision{Reason: "channel-budget"}
	}
	return availabilityMailDecision{Allow: true}
}
