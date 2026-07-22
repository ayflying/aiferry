package usage

import (
	"testing"
	"time"
)

func TestParseDashboardRangeUsesPresetDays(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 15, 30, 0, 0, location)
	dateRange, err := parseDashboardRange(now, "", "", 30)
	if err != nil {
		t.Fatalf("parseDashboardRange() error = %v", err)
	}
	if got, want := dateRange.StartAt.In(location), time.Date(2026, time.June, 21, 0, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("start = %s, want %s", got, want)
	}
	if got, want := dateRange.EndAt.In(location), time.Date(2026, time.July, 21, 0, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("end = %s, want %s", got, want)
	}
}

func TestParseDashboardRangeAcceptsCompleteCustomDays(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 15, 30, 0, 0, location)
	dateRange, err := parseDashboardRange(now, "2026-07-01", "2026-07-20", 7)
	if err != nil {
		t.Fatalf("parseDashboardRange() error = %v", err)
	}
	if got, want := dateRange.StartAt.In(location), time.Date(2026, time.July, 1, 0, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("start = %s, want %s", got, want)
	}
	if got, want := dateRange.EndAt.In(location), time.Date(2026, time.July, 21, 0, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("end = %s, want %s", got, want)
	}
}

func TestParseDashboardRangeRejectsIncompleteOrOversizedCustomRange(t *testing.T) {
	now := time.Date(2026, time.July, 20, 15, 30, 0, 0, time.Local)
	if _, err := parseDashboardRange(now, "2026-07-01", "", 7); err == nil {
		t.Fatal("expected incomplete range to be rejected")
	}
	if _, err := parseDashboardRange(now, "2026-04-21", "2026-07-20", 7); err == nil {
		t.Fatal("expected range longer than 90 days to be rejected")
	}
}
