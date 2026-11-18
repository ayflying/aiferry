package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/yunloli/aiferry/internal/logic/system"
)

func (s *sMail) NotifyAutoDisableTransition(ctx context.Context, notification system.AutoDisableNotification) {
	go s.notifyAutoDisableTransition(context.WithoutCancel(ctx), notification)
}

func (s *sMail) notifyAutoDisableTransition(ctx context.Context, notification system.AutoDisableNotification) {
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil || validateTestSettings(settings) != nil {
		return
	}
	recipients, err := s.users.AdminEmails(ctx)
	if err != nil || len(recipients) == 0 {
		return
	}
	subject, body := renderAutoDisableTransition(s.systemName(ctx), notification)
	for _, recipient := range recipients {
		_ = send(settings, recipient, subject, body)
	}
}

func renderAutoDisableTransition(systemName string, notification system.AutoDisableNotification) (string, string) {
	switch notification.Kind {
	case system.NotificationKindCredentialStatus:
		return renderCredentialStatus(systemName, notification)
	case system.NotificationKindModelAvailability:
		return renderModelAvailability(systemName, notification)
	case system.NotificationKindChannelAvailability:
		return renderChannelAvailability(systemName, notification)
	}
	target := "渠道"
	detail := fmt.Sprintf("渠道 %s（ID：%d）", notificationChannelName(notification.ChannelName), notification.ChannelID)
	if notification.CredentialID > 0 {
		target = "渠道密钥"
		detail += fmt.Sprintf("的上游密钥 %s（ID：%d）", notificationCredentialName(notification.CredentialKeyPrefix), notification.CredentialID)
	}
	if notification.ModelID > 0 {
		target = "渠道模型"
		detail += fmt.Sprintf("的模型 %s（ID：%d）", notificationModelName(notification.ModelName), notification.ModelID)
	}
	if notification.Recovered {
		return fmt.Sprintf("%s %s已自动恢复：%s", systemName, target, notificationChannelName(notification.ChannelName)),
			fmt.Sprintf("%s 已通过探测并自动恢复启用。\n\n上次禁用来源：%s\n上次禁用状态码：%s\n上次禁用原因：%s", detail, notificationSource(notification.Source), notificationStatusCode(notification.StatusCode), notificationReason(notification.Reason))
	}
	return fmt.Sprintf("%s %s已自动禁用：%s", systemName, target, notificationChannelName(notification.ChannelName)),
		fmt.Sprintf("%s 已因上游异常被自动禁用。\n\n触发来源：%s\n状态码：%s\n触发原因：%s", detail, notificationSource(notification.Source), notificationStatusCode(notification.StatusCode), notificationReason(notification.Reason))
}

// renderCredentialStatus 渲染单把上游密钥被禁用 / 恢复。
// 生产上同一个渠道会放多把密钥（每把是一个独立账号），所以标题带上渠道名，
// 正文点名具体是哪把密钥，避免只看前缀分不清。
func renderCredentialStatus(systemName string, notification system.AutoDisableNotification) (string, string) {
	name := notificationChannelName(notification.ChannelName)
	key := notificationCredentialName(notification.CredentialKeyPrefix)
	detail := fmt.Sprintf("渠道 %s（ID：%d）的上游密钥 %s（ID：%d）", name, notification.ChannelID, key, notification.CredentialID)
	if notification.Recovered {
		how := "已通过探测并自动恢复启用。"
		if notification.Manual {
			how = "已由管理端手动启用。"
		}
		return fmt.Sprintf("%s 上游密钥已恢复：%s", systemName, name),
			fmt.Sprintf("%s %s\n\n上次禁用来源：%s\n上次禁用状态码：%s\n上次禁用原因：%s",
				detail, how, notificationSource(notification.Source), notificationStatusCode(notification.StatusCode), notificationReason(notification.Reason))
	}
	how := "已因上游异常被自动禁用。"
	if notification.Manual {
		how = "已由管理端手动禁用。"
	}
	return fmt.Sprintf("%s 上游密钥已禁用：%s", systemName, name),
		fmt.Sprintf("%s %s\n\n触发来源：%s\n状态码：%s\n触发原因：%s",
			detail, how, notificationSource(notification.Source), notificationStatusCode(notification.StatusCode), notificationReason(notification.Reason))
}

// renderModelAvailability 渲染「单个模型暂时没有可用组合 / 恢复」。
// 中转渠道里一个模型挂掉不影响其它模型，因此标题只点到模型，正文里说明还有多少模型可用。
func renderModelAvailability(systemName string, notification system.AutoDisableNotification) (string, string) {
	model := notificationModelName(notification.ModelName)
	name := notificationChannelName(notification.ChannelName)
	detail := fmt.Sprintf("渠道 %s（ID：%d）的模型 %s（ID：%d）", name, notification.ChannelID, model, notification.ModelID)
	if notification.Recovered {
		return fmt.Sprintf("%s 渠道模型已恢复：%s", systemName, model),
			fmt.Sprintf("%s 已重新有可用密钥组合，恢复参与路由。", detail)
	}
	return fmt.Sprintf("%s 渠道模型不可用：%s", systemName, model),
		fmt.Sprintf("%s 在该渠道下的所有启用密钥组合都在隔离中，暂时没有可用组合；其它模型不受影响。\n\n当前仍有可用模型：%d 个",
			detail, notification.AvailableModelCount)
}

// renderChannelAvailability 渲染整个渠道不可用 / 恢复：手动开关渠道，或渠道内已无可用模型。
func renderChannelAvailability(systemName string, notification system.AutoDisableNotification) (string, string) {
	name := notificationChannelName(notification.ChannelName)
	detail := fmt.Sprintf("渠道 %s（ID：%d）", name, notification.ChannelID)
	if notification.Recovered {
		return fmt.Sprintf("%s 渠道已恢复：%s", systemName, name),
			fmt.Sprintf("%s 已重新参与路由。", detail)
	}
	if notification.Manual {
		return fmt.Sprintf("%s 渠道已被手动关闭：%s", systemName, name),
			fmt.Sprintf("%s 已在管理端被关闭，退出路由直到重新启用。", detail)
	}
	return fmt.Sprintf("%s 渠道已无可用模型：%s", systemName, name),
		fmt.Sprintf("%s 下的模型已全部不可用（每个模型的所有启用密钥组合都在隔离中），该渠道当前无法承接请求。\n\n当前可用模型：%d 个",
			detail, notification.AvailableModelCount)
}

func notificationModelName(value string) string {
	if name := strings.TrimSpace(value); name != "" {
		return name
	}
	return "未命名模型"
}

func notificationChannelName(value string) string {
	if name := strings.TrimSpace(value); name != "" {
		return name
	}
	return "未命名渠道"
}

func notificationCredentialName(value string) string {
	if keyPrefix := strings.TrimSpace(value); keyPrefix != "" {
		return keyPrefix
	}
	return "未标识密钥"
}

func notificationSource(value string) string {
	switch value {
	case system.AutoDisableSourceRelayRequest:
		return "转发请求"
	case system.AutoDisableSourceModelTest:
		return "模型测试"
	case "manual":
		return "管理端手动操作"
	default:
		return "未知"
	}
}

func notificationStatusCode(value uint) string {
	if value == 0 {
		return "未提供"
	}
	return fmt.Sprintf("%d", value)
}

func notificationReason(value string) string {
	if reason := strings.TrimSpace(value); reason != "" {
		return reason
	}
	return "未提供"
}
