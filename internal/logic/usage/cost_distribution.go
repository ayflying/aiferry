package usage

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
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

	base := dao.UsageLogs.Ctx(ctx).
		WhereGTE(dao.UsageLogs.Columns().CreatedAt, startAt).
		WhereLT(dao.UsageLogs.Columns().CreatedAt, endAt)
	var total struct {
		EstimatedCost float64 `orm:"estimated_cost"`
	}
	if err := base.Clone().Fields("COALESCE(SUM(estimated_cost),0) AS estimated_cost").Scan(&total); err != nil {
		return result, gerror.Wrap(err, "load cost distribution total")
	}
	result.TotalEstimatedCost = total.EstimatedCost

	var models []struct {
		Name          string  `orm:"name"`
		EstimatedCost float64 `orm:"estimated_cost"`
	}
	if err := base.Clone().Fields("requested_model AS name, COALESCE(SUM(estimated_cost),0) AS estimated_cost").
		Group(dao.UsageLogs.Columns().RequestedModel).OrderDesc("estimated_cost").OrderAsc(dao.UsageLogs.Columns().RequestedModel).
		Limit(recentCostModelLimit).Scan(&models); err != nil {
		return result, gerror.Wrap(err, "load cost distribution models")
	}
	if len(models) == 0 {
		return result, nil
	}

	selectedNames := make(map[string]struct{}, len(models))
	for _, model := range models {
		selectedNames[model.Name] = struct{}{}
	}
	var rows []struct {
		CreatedAt     time.Time `orm:"created_at"`
		Name          string    `orm:"name"`
		EstimatedCost float64   `orm:"estimated_cost"`
	}
	if err := base.Clone().
		Fields("created_at, requested_model AS name, COALESCE(estimated_cost,0) AS estimated_cost").
		OrderAsc("created_at").Scan(&rows); err != nil {
		return result, gerror.Wrap(err, "load cost distribution rows")
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
		bucket := costBucketStart(row.CreatedAt.In(location), startLocal, bucketUnit).Format(costBucketLayout(bucketUnit))
		costsByModel[name][bucket] += row.EstimatedCost
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
