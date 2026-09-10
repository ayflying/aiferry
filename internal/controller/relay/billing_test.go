package relay

import (
	"testing"
	"time"
)

func TestParseBillingDate(t *testing.T) {
	fallback := time.Date(2026, 9, 10, 8, 0, 0, 0, time.Local)
	if got := parseBillingDate("", fallback); !got.Equal(fallback) {
		t.Fatalf("empty value = %v, want fallback", got)
	}
	if got := parseBillingDate("not-a-date", fallback); !got.Equal(fallback) {
		t.Fatalf("invalid value = %v, want fallback", got)
	}
	got := parseBillingDate("2026-09-01", fallback)
	if got.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("parsed = %v", got)
	}
}
