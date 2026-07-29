package usage

import (
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/model/entity"
)

func TestDashboardFromUsageLogsAggregatesDAOResults(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	start := time.Date(2026, time.July, 20, 0, 0, 0, 0, location).UTC()
	rows := []entity.UsageLogs{
		{ChannelId: 1, RequestedModel: "gpt-a", HttpStatus: 200, InputTokens: 10, OutputTokens: 4, TotalTokens: 14, EstimatedCost: 1.25, DurationMs: 100, CreatedAt: start.Add(time.Hour)},
		{ChannelId: 1, RequestedModel: "gpt-a", HttpStatus: 502, InputTokens: 2, OutputTokens: 1, TotalTokens: 3, EstimatedCost: 0.5, DurationMs: 300, CreatedAt: start.Add(2 * time.Hour)},
		{ChannelId: 2, RequestedModel: "gpt-b", HttpStatus: 201, InputTokens: 6, OutputTokens: 8, TotalTokens: 14, EstimatedCost: 2, DurationMs: 200, CreatedAt: start.Add(3 * time.Hour)},
	}
	result := dashboardFromUsageLogs(rows, map[uint64]string{1: "主渠道"}, location, DashboardRange{StartAt: start, EndAt: start.Add(24 * time.Hour)}, trendBucketHour)
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
