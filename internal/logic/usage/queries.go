package usage

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	logTimeLayout        = "2006-01-02 15:04:05"
	recentCostModelLimit = 5
	otherCostModelName   = "其他模型"
)

type LogFilter struct {
	Page      int
	PageSize  int
	ModelName string
	ChannelID uint64
	APIKeyID  uint64
	UserID    uint64
	StartAt   time.Time
	EndAt     time.Time
}

func ParseLogRange(startValue, endValue string) (time.Time, time.Time, error) {
	return parseLogRange(time.Now(), startValue, endValue)
}

func (s *sUsage) ParseLogRange(ctx context.Context, startValue, endValue string) (time.Time, time.Time, error) {
	start, end, err := parseLogRange(time.Now().In(s.timeLocation(ctx)), startValue, endValue)
	return start.UTC(), end.UTC(), err
}

func parseLogRange(now time.Time, startValue, endValue string) (time.Time, time.Time, error) {
	start := startOfDay(now)
	end := start.AddDate(0, 0, 1).Add(-time.Millisecond)
	var err error
	if strings.TrimSpace(startValue) != "" {
		start, err = parseLogTime(startValue, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, gerror.New("开始时间格式无效")
		}
	}
	if strings.TrimSpace(endValue) != "" {
		end, err = parseLogTime(endValue, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, gerror.New("结束时间格式无效")
		}
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, gerror.New("结束时间不能早于开始时间")
	}
	return start, end, nil
}

func startOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func parseLogTime(value string, location *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation(logTimeLayout, value, location)
}

func (s *sUsage) Dashboard(ctx context.Context, dateRange DashboardRange) (Dashboard, error) {
	location := s.timeLocation(ctx)
	now := time.Now().In(location)
	rows := make([]entity.UsageLogs, 0)
	if err := dao.UsageLogs.Ctx(ctx).
		WhereGTE(dao.UsageLogs.Columns().CreatedAt, dateRange.StartAt).
		WhereLT(dao.UsageLogs.Columns().CreatedAt, dateRange.EndAt).
		Scan(&rows); err != nil {
		return Dashboard{}, gerror.Wrap(err, "load dashboard usage logs")
	}
	channelNames, err := loadUsageChannelNames(ctx, usageChannelIDs(rows))
	if err != nil {
		return Dashboard{}, err
	}
	trendRange := dateRange
	if current := now.UTC(); trendRange.EndAt.After(current) {
		trendRange.EndAt = current
	}
	trendBucketUnit := dashboardTrendBucketUnit(trendRange, location)
	result := dashboardFromUsageLogs(rows, channelNames, location, trendRange, trendBucketUnit)
	recentCost, err := s.costDistribution(ctx, dateRange, now, location)
	if err != nil {
		return result, err
	}
	result.RecentCost = recentCost
	return result, nil
}

func (s *sUsage) UserSummary(ctx context.Context, userID uint64, days int) (UserSummary, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	result := UserSummary{Days: days}
	start := startOfDay(time.Now().In(s.timeLocation(ctx))).AddDate(0, 0, -days+1).UTC()
	rows := make([]entity.UsageLogs, 0)
	err := dao.UsageLogs.Ctx(ctx).
		Where(dao.UsageLogs.Columns().UserId, userID).
		WhereGTE(dao.UsageLogs.Columns().CreatedAt, start).
		Scan(&rows)
	if err != nil {
		return result, gerror.Wrap(err, "load user usage logs")
	}
	return userSummaryFromUsageLogs(days, rows), nil
}

func (s *sUsage) List(ctx context.Context, input LogFilter) (LogPage, error) {
	return s.listUsageLogs(ctx, input)
}
