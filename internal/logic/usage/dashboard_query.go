package usage

import (
	"context"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
)

// 仪表盘原先会把时间区间内的所有明细行整行拉进 Go 内存做 group by，单行约 0.9 KB
// （其中 billing_details_json 平均占 596 B），7 天档就要搬运十几 MB。以下聚合查询把
// 分组下推到 SQL，只回传「桶 / 维度」级别的小结果集，数据量与区间和流量解耦。

// dashboardSummaryAggregate 是区间总量聚合的一行结果。
type dashboardSummaryAggregate struct {
	Requests      int64   `orm:"requests"`
	Successes     int64   `orm:"successes"`
	InputTokens   uint64  `orm:"input_tokens"`
	OutputTokens  uint64  `orm:"output_tokens"`
	TotalTokens   uint64  `orm:"total_tokens"`
	EstimatedCost float64 `orm:"estimated_cost"`
	DurationMs    uint64  `orm:"duration_ms"`
}

// dashboardBreakdownAggregate 是模型或渠道维度聚合的一行结果。
type dashboardBreakdownAggregate struct {
	Name          string  `orm:"name"`
	ChannelId     uint64  `orm:"channel_id"`
	Requests      int64   `orm:"requests"`
	TotalTokens   uint64  `orm:"total_tokens"`
	EstimatedCost float64 `orm:"estimated_cost"`
}

// dashboardTrendAggregate 是时间趋势按桶聚合的一行结果。
type dashboardTrendAggregate struct {
	Bucket        string  `orm:"bucket"`
	Requests      int64   `orm:"requests"`
	InputTokens   uint64  `orm:"input_tokens"`
	OutputTokens  uint64  `orm:"output_tokens"`
	EstimatedCost float64 `orm:"estimated_cost"`
}

// costBucketAggregate 是成本分布按「模型 × 时间桶」聚合的一行结果。
type costBucketAggregate struct {
	Name          string  `orm:"name"`
	Bucket        string  `orm:"bucket"`
	EstimatedCost float64 `orm:"estimated_cost"`
}

// modelGroupExpression 返回区分大小写的模型分组表达式。
//
// MySQL 的字符串比较默认忽略大小写，直接 GROUP BY requested_model 会把
// Qwen3.8-Flash 与 qwen3.8-flash 这类同形不同大小写的模型名合成一组；而改动前的
// 实现是用 Go map 按原始字符串分组，两者结论不同。BINARY 按字节比较，还原原先的
// 分组粒度。
func modelGroupExpression(column string) string {
	return "BINARY " + column
}

// modelGroupNameExpression 取组内模型名作为展示名。
//
// BINARY 分组后 SELECT 直接写列名会违反 ONLY_FULL_GROUP_BY；而该分组下同组内模型名
// 必然只有一种取值，用 ANY_VALUE 取出即可，且结果保持字符集类型而非二进制串。
func modelGroupNameExpression(column string) string {
	return "ANY_VALUE(" + column + ") AS name"
}

// usageRangeQuery 构造仪表盘区间查询，左闭右开，与明细扫描时的边界保持一致。
func usageRangeQuery(ctx context.Context, dateRange DashboardRange) *gdb.Model {
	columns := dao.UsageLogs.Columns()
	return dao.UsageLogs.Ctx(ctx).
		WhereGTE(columns.CreatedAt, dateRange.StartAt).
		WhereLT(columns.CreatedAt, dateRange.EndAt)
}

// dashboardBucketShiftSeconds 计算库内 created_at 墙钟时间与展示时区之间的偏移量（秒）。
//
// 数据库连接串使用 loc=Local，写入时按进程本地时区格式化，所以库内字面量是 time.Local
// 的墙钟时间；而分桶需要展示时区 location 的墙钟时间，两者相差
// offset(location) - offset(time.Local)。本项目部署时 TZ 与系统时区同为 Asia/Shanghai，
// 该值为 0，SQL 侧无需平移即可直接格式化。
func dashboardBucketShiftSeconds(location *time.Location) int {
	now := time.Now()
	_, localOffset := now.In(time.Local).Zone()
	_, targetOffset := now.In(location).Zone()
	return targetOffset - localOffset
}

// shiftedTimeExpression 在没有偏移时保持列名原样，避免给列套函数影响索引可用性。
func shiftedTimeExpression(column string, shiftSeconds int) string {
	if shiftSeconds == 0 {
		return column
	}
	return fmt.Sprintf("DATE_ADD(%s, INTERVAL %d SECOND)", column, shiftSeconds)
}

// trendBucketExpression 生成与 Go 侧 usageTrend 对齐的桶表达式。
func trendBucketExpression(column, bucketUnit string, shiftSeconds int) string {
	shifted := shiftedTimeExpression(column, shiftSeconds)
	if bucketUnit == trendBucketHour {
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d %%H:00:00')", shifted)
	}
	return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", shifted)
}

// costBucketExpression 生成与 Go 侧 costBucketStart 对齐的桶表达式。
//
// 天与小时按墙钟截断；周桶以区间起始本地日为锚点每 7 天一个桶，等价于 Go 侧的
// dashboardDayCount/7*7 偏移。
func costBucketExpression(column string, unit costBucketUnit, startLocal time.Time, shiftSeconds int) string {
	shifted := shiftedTimeExpression(column, shiftSeconds)
	if unit == costBucketHour {
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d %%H:00:00')", shifted)
	}
	if unit == costBucketDay {
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", shifted)
	}
	localDate := fmt.Sprintf("DATE(%s)", shifted)
	return fmt.Sprintf(
		"DATE_FORMAT(DATE_SUB(%s, INTERVAL MOD(DATEDIFF(%s, '%s'), 7) DAY), '%%Y-%%m-%%d')",
		localDate, localDate, startLocal.Format(time.DateOnly),
	)
}

func (s *sUsage) dashboardSummaryAggregate(ctx context.Context, dateRange DashboardRange) (dashboardSummaryAggregate, error) {
	columns := dao.UsageLogs.Columns()
	result := dashboardSummaryAggregate{}
	if err := usageRangeQuery(ctx, dateRange).
		Fields(
			"COUNT(*) AS requests",
			"SUM(CASE WHEN "+columns.HttpStatus+" BETWEEN 200 AND 299 THEN 1 ELSE 0 END) AS successes",
			"COALESCE(SUM("+columns.InputTokens+"), 0) AS input_tokens",
			"COALESCE(SUM("+columns.OutputTokens+"), 0) AS output_tokens",
			"COALESCE(SUM("+columns.TotalTokens+"), 0) AS total_tokens",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS estimated_cost",
			"COALESCE(SUM("+columns.DurationMs+"), 0) AS duration_ms",
		).
		Scan(&result); err != nil {
		return dashboardSummaryAggregate{}, gerror.Wrap(err, "aggregate dashboard summary")
	}
	return result, nil
}

func (s *sUsage) dashboardModelAggregates(ctx context.Context, dateRange DashboardRange) ([]dashboardBreakdownAggregate, error) {
	columns := dao.UsageLogs.Columns()
	// 模型名是字符串，库默认 collation 大小写不敏感：直接 GROUP BY 会把
	// Qwen3.8-Flash 与 qwen3.8-flash 合成一组，而原先的 Go map 是区分大小写的。
	// 用 BINARY 分组还原逐行聚合的分组粒度，再取组内原值作为展示名。
	result := make([]dashboardBreakdownAggregate, 0)
	if err := usageRangeQuery(ctx, dateRange).
		Fields(
			modelGroupNameExpression(columns.RequestedModel),
			"COUNT(*) AS requests",
			"COALESCE(SUM("+columns.TotalTokens+"), 0) AS total_tokens",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS estimated_cost",
		).
		Group(modelGroupExpression(columns.RequestedModel)).
		Scan(&result); err != nil {
		return nil, gerror.Wrap(err, "aggregate dashboard models")
	}
	return result, nil
}

func (s *sUsage) dashboardChannelAggregates(ctx context.Context, dateRange DashboardRange) ([]dashboardBreakdownAggregate, error) {
	columns := dao.UsageLogs.Columns()
	// 渠道为空的行在明细聚合里归到 0，分组表达式必须重复写出，否则 MySQL 会把
	// GROUP BY channel_id 解析成基列而把 NULL 和 0 分成两组。
	channelKey := "COALESCE(" + columns.ChannelId + ", 0)"
	result := make([]dashboardBreakdownAggregate, 0)
	if err := usageRangeQuery(ctx, dateRange).
		Fields(
			channelKey+" AS channel_id",
			"COUNT(*) AS requests",
			"COALESCE(SUM("+columns.TotalTokens+"), 0) AS total_tokens",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS estimated_cost",
		).
		Group(channelKey).
		Scan(&result); err != nil {
		return nil, gerror.Wrap(err, "aggregate dashboard channels")
	}
	return result, nil
}

func (s *sUsage) dashboardTrendAggregates(ctx context.Context, dateRange DashboardRange, bucketUnit string, shiftSeconds int) ([]dashboardTrendAggregate, error) {
	columns := dao.UsageLogs.Columns()
	bucket := trendBucketExpression(columns.CreatedAt, bucketUnit, shiftSeconds)
	result := make([]dashboardTrendAggregate, 0)
	if err := usageRangeQuery(ctx, dateRange).
		Fields(
			bucket+" AS bucket",
			"COUNT(*) AS requests",
			"COALESCE(SUM("+columns.InputTokens+"), 0) AS input_tokens",
			"COALESCE(SUM("+columns.OutputTokens+"), 0) AS output_tokens",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS estimated_cost",
		).
		Group(bucket).
		Scan(&result); err != nil {
		return nil, gerror.Wrap(err, "aggregate dashboard trend")
	}
	return result, nil
}

func (s *sUsage) costTotalAggregate(ctx context.Context, dateRange DashboardRange) (float64, error) {
	total, err := usageRangeQuery(ctx, dateRange).Sum(dao.UsageLogs.Columns().EstimatedCost)
	if err != nil {
		return 0, gerror.Wrap(err, "aggregate cost distribution total")
	}
	return total, nil
}

func (s *sUsage) costBucketAggregates(ctx context.Context, dateRange DashboardRange, unit costBucketUnit, startLocal time.Time, shiftSeconds int) ([]costBucketAggregate, error) {
	columns := dao.UsageLogs.Columns()
	bucket := costBucketExpression(columns.CreatedAt, unit, startLocal, shiftSeconds)
	result := make([]costBucketAggregate, 0)
	// GoFrame 的 Group 是覆盖语义，多列分组必须拼成一个字符串。
	if err := usageRangeQuery(ctx, dateRange).
		Fields(
			modelGroupNameExpression(columns.RequestedModel),
			bucket+" AS bucket",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS estimated_cost",
		).
		Group(modelGroupExpression(columns.RequestedModel) + ", bucket").
		Scan(&result); err != nil {
		return nil, gerror.Wrap(err, "aggregate cost distribution buckets")
	}
	return result, nil
}

// channelAggregateChannelIDs 收集渠道维度聚合用到的渠道 ID，用于批量取渠道名。
func channelAggregateChannelIDs(rows []dashboardBreakdownAggregate) []uint64 {
	ids := make(map[uint64]struct{})
	for _, row := range rows {
		if row.ChannelId > 0 {
			ids[row.ChannelId] = struct{}{}
		}
	}
	return usageReferenceIDs(ids)
}
