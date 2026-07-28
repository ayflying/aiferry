package system

import "testing"

func TestNormalizeBaseSettings(t *testing.T) {
	settings, err := normalizeBaseSettings(BaseSettings{TimeZone: " Asia/Shanghai "})
	if err != nil {
		t.Fatalf("normalizeBaseSettings() error = %v", err)
	}
	if settings.TimeZone != "Asia/Shanghai" {
		t.Fatalf("TimeZone = %q, want Asia/Shanghai", settings.TimeZone)
	}
	if _, err = normalizeBaseSettings(BaseSettings{TimeZone: "invalid/timezone"}); err == nil {
		t.Fatal("invalid timezone must be rejected")
	}
}
