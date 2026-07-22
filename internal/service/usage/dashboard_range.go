package usage

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

const maxDashboardDays = 90

// DashboardRange uses an exclusive end boundary so the selected final day is complete.
type DashboardRange struct {
	StartAt time.Time
	EndAt   time.Time
}

func (s *Service) ParseDashboardRange(ctx context.Context, startValue, endValue string, days int) (DashboardRange, error) {
	return parseDashboardRange(time.Now().In(s.timeLocation(ctx)), startValue, endValue, days)
}

func parseDashboardRange(now time.Time, startValue, endValue string, days int) (DashboardRange, error) {
	startValue = strings.TrimSpace(startValue)
	endValue = strings.TrimSpace(endValue)
	if startValue == "" && endValue == "" {
		return presetDashboardRange(now, days), nil
	}
	if startValue == "" || endValue == "" {
		return DashboardRange{}, gerror.New("自定义时间需要同时提供开始日期和结束日期")
	}

	start, err := time.ParseInLocation(time.DateOnly, startValue, now.Location())
	if err != nil {
		return DashboardRange{}, gerror.New("开始日期格式无效")
	}
	end, err := time.ParseInLocation(time.DateOnly, endValue, now.Location())
	if err != nil {
		return DashboardRange{}, gerror.New("结束日期格式无效")
	}
	if end.Before(start) {
		return DashboardRange{}, gerror.New("结束日期不能早于开始日期")
	}
	if end.After(startOfDay(now)) {
		return DashboardRange{}, gerror.New("结束日期不能晚于今天")
	}

	endExclusive := end.AddDate(0, 0, 1)
	if dashboardDayCount(start, endExclusive) > maxDashboardDays {
		return DashboardRange{}, gerror.New("自定义时间范围最多 90 天")
	}
	return DashboardRange{StartAt: start.UTC(), EndAt: endExclusive.UTC()}, nil
}

func presetDashboardRange(now time.Time, days int) DashboardRange {
	if days <= 0 || days > maxDashboardDays {
		days = 7
	}
	end := startOfDay(now).AddDate(0, 0, 1)
	return DashboardRange{StartAt: end.AddDate(0, 0, -days).UTC(), EndAt: end.UTC()}
}

func dashboardDayCount(start, end time.Time) int {
	count := 0
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		count++
	}
	return count
}
