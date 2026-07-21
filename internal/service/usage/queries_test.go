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

func TestStartOfDayUsesLocalCalendarBoundary(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	value := time.Date(2026, time.July, 20, 23, 30, 0, 0, location)
	got := startOfDay(value)
	want := time.Date(2026, time.July, 20, 0, 0, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("startOfDay() = %s, want %s", got, want)
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

func TestParseLogRangeUsesConfiguredLocationForPlainTime(t *testing.T) {
	location := time.FixedZone("JST", 9*60*60)
	now := time.Date(2026, time.July, 20, 10, 30, 0, 0, location)
	start, _, err := parseLogRange(now, "2026-07-20 00:00:00", "")
	if err != nil {
		t.Fatalf("parseLogRange() error = %v", err)
	}
	want := time.Date(2026, time.July, 20, 0, 0, 0, 0, location)
	if !start.Equal(want) {
		t.Fatalf("start = %s, want %s", start, want)
	}
}

func TestParseLogRangeRejectsReverseRange(t *testing.T) {
	_, _, err := parseLogRange(time.Now(), "2026-07-21T00:00:00Z", "2026-07-20T23:59:59Z")
	if err == nil {
		t.Fatal("expected reverse range to be rejected")
	}
}

func TestHourlyCostPointsIncludesEveryRecentHour(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 20, 9, 0, 0, 0, location)
	points := hourlyCostPoints(start, map[string]float64{
		"2026-07-20 10:00:00": 3.5,
		"2026-07-21 08:00:00": 1.25,
	})
	if len(points) != recentCostHours {
		t.Fatalf("point count = %d, want %d", len(points), recentCostHours)
	}
	if points[0].Bucket != "2026-07-20 09:00:00" || points[1].EstimatedCost != 3.5 {
		t.Fatalf("unexpected first points: %+v", points[:2])
	}
	if points[len(points)-1].Bucket != "2026-07-21 08:00:00" || points[len(points)-1].EstimatedCost != 1.25 {
		t.Fatalf("unexpected final point: %+v", points[len(points)-1])
	}
}
