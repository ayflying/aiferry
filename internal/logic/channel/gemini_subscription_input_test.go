package channel

import (
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestGeminiSubscriptionInputRequiresExplicitProxyAndProject(t *testing.T) {
	proxy, blank, token := "http://127.0.0.1:10808", " ", "manual-key"
	for _, tc := range []struct {
		name          string
		base          string
		in            adminapi.ChannelInput
		existingProxy bool
		wantOK        bool
	}{
		{"create", GeminiSubscriptionBaseURL, adminapi.ChannelInput{ProjectID: "example", ProxyURL: &proxy}, false, true},
		{"keep", GeminiSubscriptionBaseURL, adminapi.ChannelInput{ProjectID: "example"}, true, true},
		{"missing", GeminiSubscriptionBaseURL, adminapi.ChannelInput{ProjectID: "example"}, false, false},
		{"remove", GeminiSubscriptionBaseURL, adminapi.ChannelInput{ProjectID: "example", ProxyURL: &blank}, true, false},
		{"no project", GeminiSubscriptionBaseURL, adminapi.ChannelInput{ProxyURL: &proxy}, false, false},
		{"manual key", GeminiSubscriptionBaseURL, adminapi.ChannelInput{ProjectID: "example", ProxyURL: &proxy, APIKey: &token}, false, false},
		{"unknown host", "https://example.invalid", adminapi.ChannelInput{ProjectID: "example", ProxyURL: &proxy}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateGeminiSubscriptionInput(tc.base, tc.in, tc.existingProxy); (err == nil) != tc.wantOK {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}
