package system

import (
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestNormalizeMailSettings(t *testing.T) {
	settings, err := normalizeMailSettings(adminapi.MailSettingsInput{
		Enabled:         true,
		Host:            "smtp.example.com",
		Port:            587,
		Username:        "mailer",
		From:            "noreply@example.com",
		Security:        "starttls",
		Threshold:       5,
		SubjectTemplate: "余额提醒",
		BodyTemplate:    "余额为 {balance}",
	}, storedMailSettings{PasswordCipher: "cipher"})
	if err != nil {
		t.Fatalf("normalize mail settings: %v", err)
	}
	if settings.Security != "starttls" || settings.PasswordCipher != "cipher" {
		t.Fatalf("unexpected normalized settings: %#v", settings)
	}
}

func TestNormalizeMailSettingsRejectsPasswordlessUsername(t *testing.T) {
	_, err := normalizeMailSettings(adminapi.MailSettingsInput{
		Enabled:         true,
		Host:            "smtp.example.com",
		Port:            587,
		Username:        "mailer",
		From:            "noreply@example.com",
		Security:        "starttls",
		Threshold:       5,
		SubjectTemplate: "余额提醒",
		BodyTemplate:    "余额为 {balance}",
	}, storedMailSettings{})
	if err == nil {
		t.Fatal("expected missing SMTP password to be rejected")
	}
}
