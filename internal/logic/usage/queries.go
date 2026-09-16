package usage

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
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
	// 库内 created_at 是进程本地时区的墙钟时间，分桶要按展示时区，两者不一致时才平移。
	shiftSeconds := dashboardBucketShiftSeconds(location)

	summary, err := s.dashboardSummaryAggregate(ctx, dateRange)
	if err != nil {
		return Dashboard{}, err
	}
	models, err := s.dashboardModelAggregates(ctx, dateRange)
	if err != nil {
		return Dashboard{}, err
	}
	channels, err := s.dashboardChannelAggregates(ctx, dateRange)
	if err != nil {
		return Dashboard{}, err
	}
	channelNames, err := loadUsageChannelNames(ctx, channelAggregateChannelIDs(channels))
	if err != nil {
		return Dashboard{}, err
	}
	trendRange := dateRange
	if current := now.UTC(); trendRange.EndAt.After(current) {
		trendRange.EndAt = current
	}
	trendBucketUnit := dashboardTrendBucketUnit(trendRange, location)
	trendRows, err := s.dashboardTrendAggregates(ctx, dateRange, trendBucketUnit, shiftSeconds)
	if err != nil {
		return Dashboard{}, err
	}
	result := dashboardFromAggregates(
		summary, models, channels, trendRows, channelNames, location, trendRange, trendBucketUnit,
	)
	recentCost, err := s.costDistribution(ctx, dateRange, now, location)
	if err != nil {
		return result, err
	}
	result.RecentCost = recentCost
	return result, nil
}

// userSummaryAggregate 承载用户用量的单次聚合结果。
// 过滤列与取值列恰好被 idx_usage_logs_user_summary 覆盖，查询无需回表。
type userSummaryAggregate struct {
	Requests      int64   `orm:"requests"`
	Successes     int64   `orm:"successes"`
	InputTokens   uint64  `orm:"input_tokens"`
	OutputTokens  uint64  `orm:"output_tokens"`
	TotalTokens   uint64  `orm:"total_tokens"`
	EstimatedCost float64 `orm:"estimated_cost"`
}

func (s *sUsage) UserSummary(ctx context.Context, userID uint64, days int) (UserSummary, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	start := startOfDay(time.Now().In(s.timeLocation(ctx))).AddDate(0, 0, -days+1).UTC()
	columns := dao.UsageLogs.Columns()
	// 六个指标一次聚合取回：原实现按指标各发一条 COUNT/SUM，而用户列表页会对
	// 每个用户各跑一遍这一组查询，用户数增长时开销线性放大。
	var aggregate userSummaryAggregate
	if err := dao.UsageLogs.Ctx(ctx).
		Fields(
			"COUNT(*) AS requests",
			"COALESCE(SUM(CASE WHEN "+columns.HttpStatus+" BETWEEN 200 AND 299 THEN 1 ELSE 0 END), 0) AS successes",
			"COALESCE(SUM("+columns.InputTokens+"), 0) AS input_tokens",
			"COALESCE(SUM("+columns.OutputTokens+"), 0) AS output_tokens",
			"COALESCE(SUM("+columns.TotalTokens+"), 0) AS total_tokens",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS estimated_cost",
		).
		Where(columns.UserId, userID).
		WhereGTE(columns.CreatedAt, start).
		Scan(&aggregate); err != nil {
		return UserSummary{}, gerror.Wrap(err, "aggregate user usage logs")
	}
	return UserSummary{
		Days:          days,
		Requests:      aggregate.Requests,
		Successes:     aggregate.Successes,
		InputTokens:   aggregate.InputTokens,
		OutputTokens:  aggregate.OutputTokens,
		TotalTokens:   aggregate.TotalTokens,
		EstimatedCost: aggregate.EstimatedCost,
	}, nil
}

func (s *sUsage) List(ctx context.Context, input LogFilter) (LogPage, error) {
	return s.listUsageLogs(ctx, input)
}
