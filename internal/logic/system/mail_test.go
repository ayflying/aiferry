package system

import (
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func boolValue(value bool) *bool { return &value }

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

func TestNormalizeMailSettingsPreservesLegacyChannelAlertSetting(t *testing.T) {
	input := adminapi.MailSettingsInput{
		Enabled: false, Host: "smtp.example.com", Port: 587, From: "noreply@example.com",
		Security: "starttls", Threshold: 5, SubjectTemplate: "余额提醒", BodyTemplate: "余额为 {balance}",
	}
	settings, err := normalizeMailSettings(input, storedMailSettings{Enabled: true})
	if err != nil {
		t.Fatalf("normalize legacy mail settings: %v", err)
	}
	if !settings.channelAlertEnabled() {
		t.Fatal("legacy enabled setting should preserve channel alerts")
	}
}

func TestNormalizeMailSettingsUsesExplicitChannelAlertSetting(t *testing.T) {
	input := adminapi.MailSettingsInput{
		Enabled: false, ChannelAlertEnabled: boolValue(true), Host: "smtp.example.com", Port: 587,
		From: "noreply@example.com", Security: "starttls", Threshold: 5,
		SubjectTemplate: "余额提醒", BodyTemplate: "余额为 {balance}",
	}
	settings, err := normalizeMailSettings(input, storedMailSettings{})
	if err != nil {
		t.Fatalf("normalize mail settings: %v", err)
	}
	if !settings.channelAlertEnabled() {
		t.Fatal("explicit channel alert setting should be retained")
	}
}

func TestNormalizeMailSettingsNormalizesChannelBalanceThresholds(t *testing.T) {
	value := "1， 10,5,10"
	settings, err := normalizeMailSettings(adminapi.MailSettingsInput{
		ChannelBalanceThresholds: &value,
	}, storedMailSettings{})
	if err != nil {
		t.Fatalf("normalize channel balance thresholds: %v", err)
	}
	if settings.ChannelBalanceThresholds != "10,5,1" {
		t.Fatalf("unexpected normalized thresholds: %q", settings.ChannelBalanceThresholds)
	}
}

func TestNormalizeChannelBalanceThresholdsRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "10,,5", "10,0", "10,nope"} {
		if _, err := NormalizeChannelBalanceThresholds(value); err == nil {
			t.Fatalf("expected invalid thresholds %q to be rejected", value)
		}
	}
}
