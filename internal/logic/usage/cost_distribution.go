package usage

import (
	"context"
	"sort"
	"time"
)

type costBucketUnit string

const (
	costBucketHour costBucketUnit = "hour"
	costBucketDay  costBucketUnit = "day"
	costBucketWeek costBucketUnit = "week"

	maxDailyCostDistributionDays = 31
	hourBucketLayout             = "2006-01-02 15:00:00"
	dayBucketLayout              = "2006-01-02"
)

func (s *sUsage) costDistribution(ctx context.Context, dateRange DashboardRange, now time.Time, location *time.Location) (RecentCostDistribution, error) {
	endAt := dateRange.EndAt
	if current := now.UTC(); endAt.After(current) {
		endAt = current
	}
	startAt := dateRange.StartAt
	startLocal := startAt.In(location)
	endLocal := endAt.In(location)
	bucketUnit := costDistributionBucketUnit(startLocal, endLocal)
	result := RecentCostDistribution{
		BucketUnit: string(bucketUnit),
		Models:     make([]RecentCostModel, 0),
	}
	if !endAt.After(startAt) {
		return result, nil
	}

	// 区间边界沿用 clamp 后的 endAt；总量与「模型 × 桶」都在数据库侧聚合，只回传小结果集。
	scopedRange := DashboardRange{StartAt: startAt, EndAt: endAt}
	total, err := s.costTotalAggregate(ctx, scopedRange)
	if err != nil {
		return result, err
	}
	result.TotalEstimatedCost = total
	rows, err := s.costBucketAggregates(ctx, scopedRange, bucketUnit, startLocal, dashboardBucketShiftSeconds(location))
	if err != nil {
		return result, err
	}
	modelCosts := make(map[string]float64)
	for _, row := range rows {
		modelCosts[row.Name] += row.EstimatedCost
	}
	if len(modelCosts) == 0 {
		return result, nil
	}
	type modelCost struct {
		Name          string
		EstimatedCost float64
	}
	models := make([]modelCost, 0, len(modelCosts))
	for name, estimatedCost := range modelCosts {
		models = append(models, modelCost{Name: name, EstimatedCost: estimatedCost})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].EstimatedCost == models[j].EstimatedCost {
			return models[i].Name < models[j].Name
		}
		return models[i].EstimatedCost > models[j].EstimatedCost
	})
	if len(models) > recentCostModelLimit {
		models = models[:recentCostModelLimit]
	}

	selectedNames := make(map[string]struct{}, len(models))
	for _, model := range models {
		selectedNames[model.Name] = struct{}{}
	}
	costsByModel := make(map[string]map[string]float64, len(models)+1)
	hasOtherModels := false
	for _, row := range rows {
		name := row.Name
		if _, selected := selectedNames[name]; !selected {
			name = otherCostModelName
			hasOtherModels = true
		}
		if costsByModel[name] == nil {
			costsByModel[name] = make(map[string]float64)
		}
		costsByModel[name][row.Bucket] += row.EstimatedCost
	}
	for _, model := range models {
		result.Models = append(result.Models, RecentCostModel{
			Name:   model.Name,
			Points: costDistributionPoints(startLocal, endLocal, bucketUnit, costsByModel[model.Name]),
		})
	}
	if hasOtherModels {
		result.Models = append(result.Models, RecentCostModel{
			Name:   otherCostModelName,
			Points: costDistributionPoints(startLocal, endLocal, bucketUnit, costsByModel[otherCostModelName]),
		})
	}
	return result, nil
}

func costDistributionBucketUnit(start, end time.Time) costBucketUnit {
	if end.Sub(start) <= 48*time.Hour {
		return costBucketHour
	}
	if end.Sub(start) <= maxDailyCostDistributionDays*24*time.Hour {
		return costBucketDay
	}
	return costBucketWeek
}

func costDistributionPoints(start, end time.Time, unit costBucketUnit, costs map[string]float64) []CostDistributionPoint {
	points := make([]CostDistributionPoint, 0)
	for bucketStart := costBucketStart(start, start, unit); bucketStart.Before(end); bucketStart = nextCostBucket(bucketStart, unit) {
		bucket := bucketStart.Format(costBucketLayout(unit))
		points = append(points, CostDistributionPoint{Bucket: bucket, EstimatedCost: costs[bucket]})
	}
	return points
}

func costBucketStart(value, rangeStart time.Time, unit costBucketUnit) time.Time {
	switch unit {
	case costBucketHour:
		return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), 0, 0, 0, value.Location())
	case costBucketDay:
		return startOfDay(value)
	default:
		rangeDay := startOfDay(rangeStart)
		valueDay := startOfDay(value)
		days := dashboardDayCount(rangeDay, valueDay)
		return rangeDay.AddDate(0, 0, days/7*7)
	}
}

func nextCostBucket(value time.Time, unit costBucketUnit) time.Time {
	if unit == costBucketHour {
		return value.Add(time.Hour)
	}
	if unit == costBucketDay {
		return value.AddDate(0, 0, 1)
	}
	return value.AddDate(0, 0, 7)
}

func costBucketLayout(unit costBucketUnit) string {
	if unit == costBucketHour {
		return hourBucketLayout
	}
	return dayBucketLayout
}
