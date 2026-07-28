package user

import "testing"

func TestNormalizeEmail(t *testing.T) {
	value, err := normalizeEmail("User@Example.COM")
	if err != nil || value != "user@example.com" {
		t.Fatalf("normalizeEmail() = %q, %v", value, err)
	}
	if value, err = normalizeEmail(" "); err != nil || value != "" {
		t.Fatalf("empty email should be accepted: %q, %v", value, err)
	}
	if _, err = normalizeEmail("not-an-email"); err == nil {
		t.Fatal("invalid email should be rejected")
	}
}
