package usage

import (
	"testing"
	"time"
)

func TestDashboardFromAggregatesBuildsSummaryAndBreakdowns(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 20, 0, 0, 0, 0, location).UTC()
	result := dashboardFromAggregates(
		dashboardSummaryAggregate{
			Requests:      3,
			Successes:     2,
			InputTokens:   18,
			OutputTokens:  13,
			TotalTokens:   31,
			EstimatedCost: 3.75,
			DurationMs:    600,
		},
		[]dashboardBreakdownAggregate{
			{Name: "gpt-a", Requests: 2, TotalTokens: 17, EstimatedCost: 1.75},
			{Name: "gpt-b", Requests: 1, TotalTokens: 14, EstimatedCost: 2},
		},
		[]dashboardBreakdownAggregate{
			{ChannelId: 1, Requests: 2, TotalTokens: 17, EstimatedCost: 1.75},
			{ChannelId: 2, Requests: 1, TotalTokens: 14, EstimatedCost: 2},
		},
		[]dashboardTrendAggregate{
			{Bucket: "2026-07-20 09:00:00", Requests: 3, InputTokens: 18, OutputTokens: 13, EstimatedCost: 3.75},
		},
		map[uint64]string{1: "主渠道"},
		location,
		DashboardRange{StartAt: start, EndAt: start.Add(24 * time.Hour)},
		trendBucketHour,
	)
	if result.Summary.Requests != 3 || result.Summary.Successes != 2 || result.Summary.InputTokens != 18 || result.Summary.OutputTokens != 13 || result.Summary.TotalTokens != 31 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	if result.Summary.EstimatedCost == nil || *result.Summary.EstimatedCost != 3.75 || result.Summary.AverageLatency != 200 {
		t.Fatalf("unexpected cost or latency: %+v", result.Summary)
	}
	if len(result.ByModel) != 2 || result.ByModel[0].Name != "gpt-a" || result.ByModel[0].Requests != 2 {
		t.Fatalf("unexpected model breakdown: %+v", result.ByModel)
	}
	if len(result.ByChannel) != 2 || result.ByChannel[0].Name != "主渠道" || result.ByChannel[1].Name != "不可用渠道" {
		t.Fatalf("unexpected channel breakdown: %+v", result.ByChannel)
	}
}

func TestDashboardFromAggregatesKeepsEmptySummaryPointers(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 20, 0, 0, 0, 0, location).UTC()
	result := dashboardFromAggregates(
		dashboardSummaryAggregate{},
		nil, nil, nil, nil, location,
		DashboardRange{StartAt: start, EndAt: start.Add(24 * time.Hour)},
		trendBucketHour,
	)
	if result.Summary.EstimatedCost == nil {
		t.Fatal("empty summary must still expose a cost pointer, matching the previous behaviour")
	}
	if result.Summary.AverageLatency != 0 {
		t.Fatalf("average latency = %v, want 0 for empty range", result.Summary.AverageLatency)
	}
	if len(result.ByModel) != 0 || len(result.ByChannel) != 0 {
		t.Fatalf("unexpected empty breakdowns: %+v / %+v", result.ByModel, result.ByChannel)
	}
}

func TestTopUsageBreakdownsSortsThenTruncates(t *testing.T) {
	input := make([]Breakdown, 0, 10)
	for _, name := range []string{"e", "d", "c", "b", "a", "j", "i", "h", "g", "f"} {
		input = append(input, Breakdown{Name: name, Requests: 1})
	}
	input = append(input, Breakdown{Name: "hot", Requests: 100})
	input = append(input, Breakdown{Name: "warm", Requests: 100})
	result := topUsageBreakdowns(input, breakdownLimit)
	if len(result) != breakdownLimit {
		t.Fatalf("breakdown count = %d, want %d", len(result), breakdownLimit)
	}
	// 请求数相同时按名称升序，保证同分条目的顺序稳定。
	if result[0].Name != "hot" || result[1].Name != "warm" {
		t.Fatalf("unexpected leading breakdowns: %+v", result[:2])
	}
	if result[2].Name != "a" || result[7].Name != "f" {
		t.Fatalf("unexpected tail breakdowns: %+v", result[2:])
	}
}

func TestUsageTrendFromAggregatesFillsEveryHour(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 20, 0, 0, 0, 0, location).UTC()
	result := usageTrendFromAggregates(
		[]dashboardTrendAggregate{
			{Bucket: "2026-07-20 09:00:00", Requests: 2, InputTokens: 10, OutputTokens: 5, EstimatedCost: 1.5},
		},
		location,
		DashboardRange{StartAt: start, EndAt: start.Add(24 * time.Hour)},
		trendBucketHour,
	)
	if len(result) != 24 {
		t.Fatalf("hourly bucket count = %d, want 24", len(result))
	}
	if result[0].Bucket != "2026-07-20 00:00:00" || result[0].Requests != 0 || result[0].EstimatedCost == nil {
		t.Fatalf("unexpected first bucket: %+v", result[0])
	}
	if result[9].Bucket != "2026-07-20 09:00:00" || result[9].Requests != 2 || *result[9].EstimatedCost != 1.5 {
		t.Fatalf("unexpected populated bucket: %+v", result[9])
	}
}

func TestUsageTrendFromAggregatesSortsDailyBuckets(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, location).UTC()
	result := usageTrendFromAggregates(
		[]dashboardTrendAggregate{
			{Bucket: "2026-07-03", Requests: 1},
			{Bucket: "2026-07-01", Requests: 2},
		},
		location,
		DashboardRange{StartAt: start, EndAt: start.AddDate(0, 0, 30)},
		trendBucketDay,
	)
	if len(result) != 2 {
		t.Fatalf("daily bucket count = %d, want 2", len(result))
	}
	if result[0].Bucket != "2026-07-01" || result[1].Bucket != "2026-07-03" {
		t.Fatalf("unexpected bucket order: %+v", result)
	}
}
