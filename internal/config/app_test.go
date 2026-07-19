package config

import (
	"encoding/base64"
	"testing"
)

func TestLoadUsesSevenDaySessionByDefault(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "test-password")
	t.Setenv("AIFERRY_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("CASDOOR_ENDPOINT", "https://casdoor.example.test")
	t.Setenv("CASDOOR_CLIENT_ID", "test-client")
	t.Setenv("CASDOOR_CLIENT_SECRET", "test-secret")
	t.Setenv("SESSION_TTL_HOURS", "")

	app, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	const sevenDaysInHours = 24 * 7
	if defaultSessionTTLHours != sevenDaysInHours {
		t.Fatalf("defaultSessionTTLHours = %d, want %d", defaultSessionTTLHours, sevenDaysInHours)
	}
	if app.SessionTTL != sevenDaysInHours {
		t.Fatalf("SessionTTL = %d, want %d", app.SessionTTL, sevenDaysInHours)
	}
}
