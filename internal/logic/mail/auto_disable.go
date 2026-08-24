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
	target := "渠道"
	detail := fmt.Sprintf("渠道 %s（ID：%d）", notificationChannelName(notification.ChannelName), notification.ChannelID)
	if notification.CredentialID > 0 {
		target = "渠道密钥"
		detail += fmt.Sprintf("的上游密钥 %s（ID：%d）", notificationCredentialName(notification.CredentialKeyPrefix), notification.CredentialID)
	}
	if notification.Recovered {
		return fmt.Sprintf("%s %s已自动恢复：%s", systemName, target, notificationChannelName(notification.ChannelName)),
			fmt.Sprintf("%s 已通过探测并自动恢复启用。\n\n上次禁用来源：%s\n上次禁用状态码：%s\n上次禁用原因：%s", detail, notificationSource(notification.Source), notificationStatusCode(notification.StatusCode), notificationReason(notification.Reason))
	}
	return fmt.Sprintf("%s %s已自动禁用：%s", systemName, target, notificationChannelName(notification.ChannelName)),
		fmt.Sprintf("%s 已因上游异常被自动禁用。\n\n触发来源：%s\n状态码：%s\n触发原因：%s", detail, notificationSource(notification.Source), notificationStatusCode(notification.StatusCode), notificationReason(notification.Reason))
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
