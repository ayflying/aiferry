package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/gogf/gf/v2/net/ghttp"
)

func TestAccountAllowed(t *testing.T) {
	tests := []struct {
		name    string
		account casdoorAccount
		allowed bool
	}{
		{name: "administrator", account: casdoorAccount{IsAdmin: true}, allowed: true},
		{name: "global administrator", account: casdoorAccount{IsGlobalAdmin: true}, allowed: true},
		{name: "group with organization", account: casdoorAccount{Groups: []string{"built-in/AI用户组"}}, allowed: true},
		{name: "plain group", account: casdoorAccount{Groups: []string{"AI用户组"}}, allowed: true},
		{name: "different group", account: casdoorAccount{Groups: []string{"built-in/研发组"}}, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := accountAllowed(test.account, "AI用户组"); got != test.allowed {
				t.Fatalf("accountAllowed() = %v, want %v", got, test.allowed)
			}
		})
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
