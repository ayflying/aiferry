package usage

import (
	"testing"
	"time"
)

func TestCostDistributionBucketUnitFollowsRangeLength(t *testing.T) {
	start := time.Date(2026, time.July, 20, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		end  time.Time
		want costBucketUnit
	}{
		{name: "24 hours", end: start.Add(24 * time.Hour), want: costBucketHour},
		{name: "seven days", end: start.AddDate(0, 0, 7), want: costBucketDay},
		{name: "thirty days", end: start.AddDate(0, 0, 30), want: costBucketWeek},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := costDistributionBucketUnit(start, test.end); got != test.want {
				t.Fatalf("costDistributionBucketUnit() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCostDistributionPointsUseCompleteBuckets(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 20, 9, 30, 0, 0, location)
	end := start.Add(24 * time.Hour)
	points := costDistributionPoints(start, end, costBucketHour, map[string]float64{
		"2026-07-20 10:00:00": 3.5,
		"2026-07-21 09:00:00": 1.25,
	})
	if len(points) != 25 {
		t.Fatalf("point count = %d, want 25", len(points))
	}
	if points[0].Bucket != "2026-07-20 09:00:00" || points[1].EstimatedCost != 3.5 {
		t.Fatalf("unexpected first points: %+v", points[:2])
	}
	if points[len(points)-1].Bucket != "2026-07-21 09:00:00" || points[len(points)-1].EstimatedCost != 1.25 {
		t.Fatalf("unexpected final point: %+v", points[len(points)-1])
	}
}

func TestCostDistributionPointsUseSevenDayBucketsForLongRanges(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, location)
	points := costDistributionPoints(start, start.AddDate(0, 0, 30), costBucketWeek, nil)
	if len(points) != 5 {
		t.Fatalf("point count = %d, want 5", len(points))
	}
	if points[0].Bucket != "2026-07-01" || points[4].Bucket != "2026-07-29" {
		t.Fatalf("unexpected buckets: %+v", points)
	}
}
