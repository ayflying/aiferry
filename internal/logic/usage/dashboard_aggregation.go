package usage

import (
	"sort"
	"time"

	"github.com/yunloli/aiferry/internal/model/entity"
)

func dashboardFromUsageLogs(rows []entity.UsageLogs, channelNames map[uint64]string, location *time.Location, dateRange DashboardRange, bucketUnit string) Dashboard {
	result := Dashboard{TrendBucketUnit: bucketUnit}
	modelBreakdowns := make(map[string]Breakdown)
	channelBreakdowns := make(map[uint64]Breakdown)
	durationTotal := uint64(0)
	estimatedCost := 0.0
	for _, row := range rows {
		result.Summary.Requests++
		if row.HttpStatus >= 200 && row.HttpStatus <= 299 {
			result.Summary.Successes++
		}
		result.Summary.InputTokens += row.InputTokens
		result.Summary.OutputTokens += row.OutputTokens
		result.Summary.TotalTokens += row.TotalTokens
		estimatedCost += row.EstimatedCost
		durationTotal += row.DurationMs
		modelBreakdowns[row.RequestedModel] = addUsageBreakdown(modelBreakdowns[row.RequestedModel], row.RequestedModel, row)
		channelName := channelNames[row.ChannelId]
		if channelName == "" {
			channelName = "不可用渠道"
		}
		channelBreakdowns[row.ChannelId] = addUsageBreakdown(channelBreakdowns[row.ChannelId], channelName, row)
	}
	result.Summary.EstimatedCost = &estimatedCost
	if result.Summary.Requests > 0 {
		result.Summary.AverageLatency = float64(durationTotal) / float64(result.Summary.Requests)
	}
	result.Trend = usageTrend(rows, location, dateRange, bucketUnit)
	result.ByModel = topUsageBreakdowns(modelUsageBreakdownValues(modelBreakdowns), 8)
	result.ByChannel = topUsageBreakdowns(channelUsageBreakdownValues(channelBreakdowns), 8)
	return result
}

func addUsageBreakdown(current Breakdown, name string, row entity.UsageLogs) Breakdown {
	if current.EstimatedCost == nil {
		current.Name = name
		current.EstimatedCost = new(float64)
	}
	current.Requests++
	current.TotalTokens += row.TotalTokens
	*current.EstimatedCost += row.EstimatedCost
	return current
}

func topUsageBreakdowns(result []Breakdown, limit int) []Breakdown {
	sort.Slice(result, func(i, j int) bool {
		if result[i].Requests == result[j].Requests {
			return result[i].Name < result[j].Name
		}
		return result[i].Requests > result[j].Requests
	})
	if len(result) > limit {
		return result[:limit]
	}
	return result
}

func modelUsageBreakdownValues(values map[string]Breakdown) []Breakdown {
	result := make([]Breakdown, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func channelUsageBreakdownValues(values map[uint64]Breakdown) []Breakdown {
	result := make([]Breakdown, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func usageTrend(rows []entity.UsageLogs, location *time.Location, dateRange DashboardRange, bucketUnit string) []TrendPoint {
	bucketLayout := time.DateOnly
	if bucketUnit == trendBucketHour {
		bucketLayout = "2006-01-02 15:00:00"
	}
	values := make(map[string]TrendPoint)
	for _, row := range rows {
		bucket := row.CreatedAt.In(location).Format(bucketLayout)
		point := values[bucket]
		point.Bucket = bucket
		point.Requests++
		point.InputTokens += row.InputTokens
		point.OutputTokens += row.OutputTokens
		if point.EstimatedCost == nil {
			point.EstimatedCost = new(float64)
		}
		*point.EstimatedCost += row.EstimatedCost
		values[bucket] = point
	}
	if bucketUnit == trendBucketHour {
		start := dateRange.StartAt.In(location)
		start = time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, location)
		end := dateRange.EndAt.In(location)
		result := make([]TrendPoint, 0, 25)
		for bucketTime := start; bucketTime.Before(end); bucketTime = bucketTime.Add(time.Hour) {
			bucket := bucketTime.Format(bucketLayout)
			point, exists := values[bucket]
			if !exists {
				point = TrendPoint{Bucket: bucket, EstimatedCost: new(float64)}
			}
			result = append(result, point)
		}
		return result
	}
	buckets := make([]string, 0, len(values))
	for bucket := range values {
		buckets = append(buckets, bucket)
	}
	sort.Strings(buckets)
	result := make([]TrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, values[bucket])
	}
	return result
}

func userSummaryFromUsageLogs(days int, rows []entity.UsageLogs) UserSummary {
	result := UserSummary{Days: days}
	for _, row := range rows {
		result.Requests++
		if row.HttpStatus >= 200 && row.HttpStatus <= 299 {
			result.Successes++
		}
		result.InputTokens += row.InputTokens
		result.OutputTokens += row.OutputTokens
		result.TotalTokens += row.TotalTokens
		result.EstimatedCost += row.EstimatedCost
	}
	return result
}
