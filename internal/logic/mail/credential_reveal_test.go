package mail

import (
	"regexp"
	"testing"
	"time"
)

func TestGenerateCredentialRevealCodeIsSixDigits(t *testing.T) {
	matched := regexp.MustCompile(`^\d{6}$`)
	for i := 0; i < 200; i++ {
		code, err := generateCredentialRevealCode()
		if err != nil {
			t.Fatal(err)
		}
		if !matched.MatchString(code) {
			t.Fatalf("expected six digit code, got %q", code)
		}
	}
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"alice@example.com":  "a***@example.com",
		"  bob@x.cn  ":       "b***@x.cn",
		"a@b.c":              "***@b.c",
		"":                   "",
		"not-an-email":       "",
		"@missing-local.com": "",
	}
	for input, want := range cases {
		if got := maskEmail(input); got != want {
			t.Fatalf("maskEmail(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCredentialRevealTTLs(t *testing.T) {
	if credentialRevealVerifiedTTL != 10*time.Minute {
		t.Fatalf("verified window must be 10 minutes, got %v", credentialRevealVerifiedTTL)
	}
	if credentialRevealCodeTTL != 5*time.Minute {
		t.Fatalf("code TTL must be 5 minutes, got %v", credentialRevealCodeTTL)
	}
	if credentialRevealSendCooldown != time.Minute {
		t.Fatalf("send cooldown must be 60 seconds, got %v", credentialRevealSendCooldown)
	}
}
