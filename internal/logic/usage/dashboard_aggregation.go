package usage

import (
	"sort"
	"time"
)

// breakdownLimit 限制模型与渠道两个维度的展示条数。
const breakdownLimit = 8

// dashboardFromAggregates 用 SQL 聚合结果组装仪表盘。
//
// 聚合口径与原先把全部明细行拉进内存再统计的实现保持一致：
// 总量、模型/渠道维度、趋势桶都由 dashboard_query.go 在数据库侧完成分组。
func dashboardFromAggregates(
	summary dashboardSummaryAggregate,
	models []dashboardBreakdownAggregate,
	channels []dashboardBreakdownAggregate,
	trendRows []dashboardTrendAggregate,
	channelNames map[uint64]string,
	location *time.Location,
	dateRange DashboardRange,
	bucketUnit string,
) Dashboard {
	estimatedCost := summary.EstimatedCost
	result := Dashboard{
		TrendBucketUnit: bucketUnit,
		Summary: Summary{
			Requests:      summary.Requests,
			Successes:     summary.Successes,
			InputTokens:   summary.InputTokens,
			OutputTokens:  summary.OutputTokens,
			TotalTokens:   summary.TotalTokens,
			EstimatedCost: &estimatedCost,
		},
	}
	if summary.Requests > 0 {
		result.Summary.AverageLatency = float64(summary.DurationMs) / float64(summary.Requests)
	}
	result.Trend = usageTrendFromAggregates(trendRows, location, dateRange, bucketUnit)
	result.ByModel = topUsageBreakdowns(modelBreakdownsFromAggregates(models), breakdownLimit)
	result.ByChannel = topUsageBreakdowns(channelBreakdownsFromAggregates(channels, channelNames), breakdownLimit)
	return result
}

func modelBreakdownsFromAggregates(rows []dashboardBreakdownAggregate) []Breakdown {
	result := make([]Breakdown, 0, len(rows))
	for _, row := range rows {
		estimatedCost := row.EstimatedCost
		result = append(result, Breakdown{
			Name:          row.Name,
			Requests:      row.Requests,
			TotalTokens:   row.TotalTokens,
			EstimatedCost: &estimatedCost,
		})
	}
	return result
}

func channelBreakdownsFromAggregates(rows []dashboardBreakdownAggregate, channelNames map[uint64]string) []Breakdown {
	result := make([]Breakdown, 0, len(rows))
	for _, row := range rows {
		name := channelNames[row.ChannelId]
		if name == "" {
			name = "不可用渠道"
		}
		estimatedCost := row.EstimatedCost
		result = append(result, Breakdown{
			Name:          name,
			Requests:      row.Requests,
			TotalTokens:   row.TotalTokens,
			EstimatedCost: &estimatedCost,
		})
	}
	return result
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

// usageTrendFromAggregates 把趋势聚合结果补齐成连续桶序列。
//
// 小时档必须补齐区间内每个整点（含没有请求的桶）；天档只输出有数据的桶并按其排序。
func usageTrendFromAggregates(rows []dashboardTrendAggregate, location *time.Location, dateRange DashboardRange, bucketUnit string) []TrendPoint {
	bucketLayout := time.DateOnly
	if bucketUnit == trendBucketHour {
		bucketLayout = "2006-01-02 15:00:00"
	}
	values := make(map[string]TrendPoint, len(rows))
	for _, row := range rows {
		estimatedCost := row.EstimatedCost
		values[row.Bucket] = TrendPoint{
			Bucket:        row.Bucket,
			Requests:      row.Requests,
			InputTokens:   row.InputTokens,
			OutputTokens:  row.OutputTokens,
			EstimatedCost: &estimatedCost,
		}
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
