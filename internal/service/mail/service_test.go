package mail

import (
	"testing"

	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/user"
)

func TestRenderTemplates(t *testing.T) {
	settings := system.MailDeliverySettings{MailSettings: system.MailSettings{
		SubjectTemplate: "余额提醒：{nickname}",
		BodyTemplate:    "余额 {balance}，阈值 {threshold}，用户 {nickname}",
		Threshold:       5,
	}}
	subject, body := renderTemplates(settings, user.Profile{Nickname: "测试用户", Balance: 1.25})
	if subject != "余额提醒：测试用户" {
		t.Fatalf("unexpected subject: %q", subject)
	}
	if body != "余额 1.250000，阈值 5.000000，用户 测试用户" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestValidRecipient(t *testing.T) {
	if !validRecipient("user@example.com") {
		t.Fatal("expected direct email address to be valid")
	}
	if validRecipient("User <user@example.com>") {
		t.Fatal("expected display-name address to be rejected")
	}
}

func TestValidateTestSettingsDoesNotRequireAlertsEnabled(t *testing.T) {
	settings := system.MailDeliverySettings{MailSettings: system.MailSettings{
		Enabled: false, Host: "smtp.example.com", Port: 587, From: "noreply@example.com",
	}}
	if err := validateTestSettings(settings); err != nil {
		t.Fatalf("disabled alerts should still allow SMTP testing: %v", err)
	}
}

func TestValidateTestSettingsReportsMissingSMTPConfiguration(t *testing.T) {
	settings := system.MailDeliverySettings{MailSettings: system.MailSettings{Port: 587, From: "noreply@example.com"}}
	if err := validateTestSettings(settings); err == nil || err.Error() != "请先保存 SMTP 主机" {
		t.Fatalf("unexpected validation result: %v", err)
	}
}

func TestChannelBalanceReminderKey(t *testing.T) {
	key := channelBalanceReminderKey(42, 5)
	if key != "aiferry:mail:channel-low-balance:42:5" {
		t.Fatalf("unexpected channel balance reminder key: %q", key)
	}
}

func TestChannelBalanceReminderKeyPreservesDecimalThreshold(t *testing.T) {
	key := channelBalanceReminderKey(42, 0.5)
	if key != "aiferry:mail:channel-low-balance:42:0.5" {
		t.Fatalf("unexpected channel balance reminder key: %q", key)
	}
}
