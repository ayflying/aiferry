package redemption

import (
	"regexp"
	"testing"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestGenerateCode(t *testing.T) {
	first, err := generateCode()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateCode()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^AFR-(?:[0-9A-F]{4}-){5}[0-9A-F]{4}$`).MatchString(first) {
		t.Fatalf("unexpected code format: %s", first)
	}
	if first == second {
		t.Fatal("generated duplicate redemption codes")
	}
}

func TestNormalizeCreateInput(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	name, amount, normalizedExpiry, err := normalizeCreateInput(adminapi.RedemptionCodeCreateInput{
		Name: "  测试额度  ", Amount: 2.5, ExpiresAt: &expiresAt, Quantity: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "测试额度" || amount.StringFixed(8) != "2.50000000" {
		t.Fatalf("unexpected normalized input: %q %s", name, amount)
	}
	if normalizedExpiry == nil || !normalizedExpiry.Equal(expiresAt.UTC()) {
		t.Fatalf("unexpected expiration: %v", normalizedExpiry)
	}
}

func TestNormalizeCreateInputRejectsInvalidValues(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	tests := []adminapi.RedemptionCodeCreateInput{
		{Name: "", Amount: 1, Quantity: 1},
		{Name: "test", Amount: 0, Quantity: 1},
		{Name: "test", Amount: 1, Quantity: 0},
		{Name: "test", Amount: 1, Quantity: 101},
		{Name: "test", Amount: 1_000_000_000_000, Quantity: 1},
		{Name: "test", Amount: 1, Quantity: 1, ExpiresAt: &past},
	}
	for _, input := range tests {
		if _, _, _, err := normalizeCreateInput(input); err == nil {
			t.Fatalf("expected validation error for %+v", input)
		}
	}
}

func TestCodeStatus(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	redeemed := now.Add(-time.Minute)
	tests := []struct {
		row  codeRow
		want string
	}{
		{row: codeRow{}, want: statusActive},
		{row: codeRow{ExpiresAt: &future}, want: statusActive},
		{row: codeRow{ExpiresAt: &past}, want: statusExpired},
		{row: codeRow{ExpiresAt: &past, RedeemedAt: &redeemed}, want: statusUsed},
	}
	for _, test := range tests {
		if got := codeStatus(test.row, now); got != test.want {
			t.Fatalf("codeStatus() = %q, want %q", got, test.want)
		}
	}
}
