package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/gogf/gf/v2/net/ghttp"
)

func TestAccountRole(t *testing.T) {
	if role := accountRole(casdoorAccount{IsAdmin: true}); role != "admin" {
		t.Fatalf("accountRole() = %q, want admin", role)
	}
	if role := accountRole(casdoorAccount{IsGlobalAdmin: true}); role != "admin" {
		t.Fatalf("accountRole() = %q, want admin", role)
	}
	if role := accountRole(casdoorAccount{}); role != "user" {
		t.Fatalf("accountRole() = %q, want user", role)
	}
}

func TestAccountDisabled(t *testing.T) {
	disabled := false
	tests := []casdoorAccount{
		{IsForbidden: true},
		{IsDeleted: true},
		{DeletedTime: "2026-01-01T00:00:00Z"},
		{Enabled: &disabled},
		{Status: "disabled"},
	}
	for index, account := range tests {
		if !accountDisabled(account) {
			t.Fatalf("case %d should be disabled", index)
		}
	}
	if accountDisabled(casdoorAccount{}) {
		t.Fatal("empty account should not be treated as disabled")
	}
}

func TestSanitizeReturnTo(t *testing.T) {
	if got := sanitizeReturnTo("/channels?tab=models"); got != "/channels?tab=models" {
		t.Fatalf("unexpected local return path %q", got)
	}
	for _, value := range []string{"https://example.com", "//example.com", "", "/ok\r\nLocation: https://example.com"} {
		if got := sanitizeReturnTo(value); got != "/" {
			t.Fatalf("sanitizeReturnTo(%q) = %q", value, got)
		}
	}
}

func TestCallbackURLPreservesHostPort(t *testing.T) {
	request := httptest.NewRequest("GET", "http://192.168.50.217:8080/api/auth/login", nil)
	callbackURL, err := CallbackURL(&ghttp.Request{Request: request})
	if err != nil {
		t.Fatalf("CallbackURL() error = %v", err)
	}
	if callbackURL != "http://192.168.50.217:8080/auth/casdoor/callback" {
		t.Fatalf("CallbackURL() = %q", callbackURL)
	}
}

func TestCallbackURLUsesForwardedProtocol(t *testing.T) {
	request := httptest.NewRequest("GET", "http://aiferry.example.com/api/auth/login", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	callbackURL, err := CallbackURL(&ghttp.Request{Request: request})
	if err != nil {
		t.Fatalf("CallbackURL() error = %v", err)
	}
	if callbackURL != "https://aiferry.example.com/auth/casdoor/callback" {
		t.Fatalf("CallbackURL() = %q", callbackURL)
	}
}
