package usage

import (
	"testing"
	"time"
)

func TestParseLogRangeDefaultsToFullDay(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 10, 30, 0, 0, location)
	start, end, err := parseLogRange(now, "", "")
	if err != nil {
		t.Fatalf("parseLogRange() error = %v", err)
	}
	if want := time.Date(2026, time.July, 20, 0, 0, 0, 0, location); !start.Equal(want) {
		t.Fatalf("start = %s, want %s", start, want)
	}
	if want := time.Date(2026, time.July, 20, 23, 59, 59, int(999*time.Millisecond), location); !end.Equal(want) {
		t.Fatalf("end = %s, want %s", end, want)
	}
}

func TestParseLogRangeAcceptsISOTime(t *testing.T) {
	start, end, err := parseLogRange(time.Now(), "2026-07-20T00:00:00+08:00", "2026-07-20T23:59:59.999+08:00")
	if err != nil {
		t.Fatalf("parseLogRange() error = %v", err)
	}
	if start.Format(time.RFC3339) != "2026-07-20T00:00:00+08:00" || end.Format(time.RFC3339Nano) != "2026-07-20T23:59:59.999+08:00" {
		t.Fatalf("unexpected range: %s to %s", start, end)
	}
}

func TestParseLogRangeRejectsReverseRange(t *testing.T) {
	_, _, err := parseLogRange(time.Now(), "2026-07-21T00:00:00Z", "2026-07-20T23:59:59Z")
	if err == nil {
		t.Fatal("expected reverse range to be rejected")
	}
}
