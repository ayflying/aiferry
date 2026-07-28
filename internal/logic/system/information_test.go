package system

import (
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestNormalizeSystemInformation(t *testing.T) {
	settings, err := normalizeSystemInformation(adminapi.SystemInformationInput{
		SystemName:  "  My Gateway  ",
		ServerURL:   "https://gateway.example.com/",
		LogoURL:     "https://cdn.example.com/logo.png",
		HomeContent: "    markdown code block",
	})
	if err != nil {
		t.Fatalf("normalizeSystemInformation() error = %v", err)
	}
	if settings.SystemName != "My Gateway" || settings.ServerURL != "https://gateway.example.com" {
		t.Fatalf("unexpected normalized settings: %#v", settings)
	}
	if settings.HomeContent != "    markdown code block" {
		t.Fatalf("markdown indentation must be preserved, got %q", settings.HomeContent)
	}
}

func TestNormalizeSystemInformationRejectsUnsafeURLs(t *testing.T) {
	for _, input := range []adminapi.SystemInformationInput{
		{SystemName: "Gateway", ServerURL: "https://gateway.example.com/path"},
		{SystemName: "Gateway", ServerURL: "https://user@gateway.example.com"},
		{SystemName: "Gateway", LogoURL: "javascript:alert(1)"},
		{SystemName: "Gateway", LogoURL: "https://cdn.example.com/logo.png\r\nX-Test: 1"},
	} {
		if _, err := normalizeSystemInformation(input); err == nil {
			t.Fatalf("unsafe input must be rejected: %#v", input)
		}
	}
}

func TestResolveSystemInformationFallbackURL(t *testing.T) {
	url, err := normalizeRootHTTPURL("https://gateway.example.com", false)
	if err != nil || url != "https://gateway.example.com" {
		t.Fatalf("normalizeRootHTTPURL() = %q, %v", url, err)
	}
}
