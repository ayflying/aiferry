package system

import (
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestDecodeRequestFirewallSettingsUsesSafeDefaults(t *testing.T) {
	settings, err := decodeRequestFirewallSettings([]byte(`{"enabled":false,"maxConcurrentRequests":128,"maxConcurrentRequestsPerIp":16,"maxConcurrentRequestsPerKey":32,"requestsPerMinutePerIp":240,"requestsPerMinutePerApiKey":240}`))
	if err != nil {
		t.Fatal(err)
	}
	if settings.Enabled || settings.MaxConcurrentRequests != 128 || settings.MaxConcurrentRequestsPerIP != 16 || settings.MaxConcurrentRequestsPerKey != 32 || settings.RequestsPerMinutePerIP != 240 || settings.RequestsPerMinutePerAPIKey != 240 {
		t.Fatalf("unexpected settings: %#v", settings)
	}
}

func TestNormalizeRequestFirewallSettingsRejectsInvalidLimits(t *testing.T) {
	settings := DefaultRequestFirewallSettings()
	settings.MaxConcurrentRequestsPerIP = settings.MaxConcurrentRequests + 1
	if _, err := normalizeRequestFirewallSettings(settings); err == nil {
		t.Fatal("per-IP concurrent limit above global limit must be rejected")
	}
	if _, err := normalizeRequestFirewallSettings(adminapi.RequestFirewallSettingsInput{}); err == nil {
		t.Fatal("zero limits must be rejected")
	}
}
