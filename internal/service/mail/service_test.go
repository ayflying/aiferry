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
