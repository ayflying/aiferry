package channel

import (
	"testing"

	"github.com/gogf/gf/v2/os/gtime"
)

func TestSummarizeCredentialHealth(t *testing.T) {
	now := gtime.NewFromStr("2024-01-01 12:00:00")
	past := gtime.NewFromStr("2024-01-01 11:00:00")
	future := gtime.NewFromStr("2024-01-01 13:00:00")

	t.Run("nil views -> no available", func(t *testing.T) {
		score, available := summarizeCredentialHealth(nil, now)
		if available || score != 0 {
			t.Fatalf("expected (0, false), got (%d, %v)", score, available)
		}
	})

	t.Run("empty views -> no available", func(t *testing.T) {
		score, available := summarizeCredentialHealth([]*CredentialHealthView{}, now)
		if available || score != 0 {
			t.Fatalf("expected (0, false), got (%d, %v)", score, available)
		}
	})

	t.Run("no combo rows default 100, excludes cooling", func(t *testing.T) {
		views := []*CredentialHealthView{
			{CredentialId: 1, HealthScore: 100},
			{CredentialId: 2, HealthScore: 30, CooldownUntil: future},
			{CredentialId: 3, HealthScore: 80, CooldownUntil: past},
		}
		score, available := summarizeCredentialHealth(views, now)
		if !available || score != 100 {
			t.Fatalf("expected (100, true), got (%d, %v)", score, available)
		}
	})

	t.Run("cooling beats available lower score", func(t *testing.T) {
		views := []*CredentialHealthView{
			{CredentialId: 1, HealthScore: 100, CooldownUntil: future},
			{CredentialId: 2, HealthScore: 70, CooldownUntil: past},
		}
		score, available := summarizeCredentialHealth(views, now)
		if !available || score != 70 {
			t.Fatalf("expected (70, true), got (%d, %v)", score, available)
		}
	})

	t.Run("all cooling -> no available, score 0", func(t *testing.T) {
		views := []*CredentialHealthView{
			{CredentialId: 1, HealthScore: 100, CooldownUntil: future},
			{CredentialId: 2, HealthScore: 90, CooldownUntil: future},
		}
		score, available := summarizeCredentialHealth(views, now)
		if available || score != 0 {
			t.Fatalf("expected (0, false), got (%d, %v)", score, available)
		}
	})

	t.Run("expired cooldown counts as available", func(t *testing.T) {
		views := []*CredentialHealthView{
			{CredentialId: 1, HealthScore: 55, CooldownUntil: past},
		}
		score, available := summarizeCredentialHealth(views, now)
		if !available || score != 55 {
			t.Fatalf("expected (55, true), got (%d, %v)", score, available)
		}
	})
}

func TestSanitizeCredentialErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"sk prefix", "sk-abcd1234efgh leaked", "sk-abc**** leaked"},
		{"af prefix", "key af_xyz9876543210 exposed", "key af_xyz**** exposed"},
		{"bearer token", "Bearer abcdef1234567890", "Bearer ****"},
		{"plain text untouched", "upstream timeout after 30s", "upstream timeout after 30s"},
		{"uuid untouched", "id 12345678-1234-1234-1234-123456789012 failed", "id 12345678-1234-1234-1234-123456789012 failed"},
		{"long random redacted", "secret ZmKrZV9zZWNyZXRfdmFsdWUxMjM0NTY3OA== here", "secret ZmKrZV9z**** here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeCredentialErrorMessage(c.in); got != c.want {
				t.Fatalf("sanitizeCredentialErrorMessage(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
