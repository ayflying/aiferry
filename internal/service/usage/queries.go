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
	hourBucketLayout     = "2006-01-02 15:00:00"
	recentCostHours      = 24
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

func parseLogRange(now time.Time, startValue, endValue string) (time.Time, time.Time, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1).Add(-time.Millisecond)
	var err error
	if strings.TrimSpace(startValue) != "" {
		start, err = parseLogTime(startValue)
		if err != nil {
			return time.Time{}, time.Time{}, gerror.New("开始时间格式无效")
		}
	}
	if strings.TrimSpace(endValue) != "" {
		end, err = parseLogTime(endValue)
		if err != nil {
			return time.Time{}, time.Time{}, gerror.New("结束时间格式无效")
		}
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, gerror.New("结束时间不能早于开始时间")
	}
	return start, end, nil
}

func parseLogTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation(logTimeLayout, value, time.Local)
}

func (s *Service) Dashboard(ctx context.Context, days int) (Dashboard, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	now := time.Now()
	start := now.AddDate(0, 0, -days+1).Truncate(24 * time.Hour)
	var result Dashboard
	base := dao.UsageLogs.Ctx(ctx).WhereGTE(dao.UsageLogs.Columns().CreatedAt, start)
	if err := base.Clone().Fields(`
		COUNT(*) AS requests,
		COALESCE(SUM(CASE WHEN http_status BETWEEN 200 AND 299 THEN 1 ELSE 0 END),0) AS successes,
		COALESCE(SUM(input_tokens),0) AS input_tokens,
		COALESCE(SUM(output_tokens),0) AS output_tokens,
		COALESCE(SUM(total_tokens),0) AS total_tokens,
		COALESCE(SUM(estimated_cost),0) AS estimated_cost,
		COALESCE(AVG(duration_ms),0) AS average_latency`).Scan(&result.Summary); err != nil {
		return result, gerror.Wrap(err, "load dashboard summary")
	}
	if err := base.Clone().Fields(`
		DATE_FORMAT(created_at,'%Y-%m-%d') AS bucket,
		COUNT(*) AS requests,
		COALESCE(SUM(input_tokens),0) AS input_tokens,
		COALESCE(SUM(output_tokens),0) AS output_tokens,
		COALESCE(SUM(estimated_cost),0) AS estimated_cost`).
		Group("bucket").OrderAsc("bucket").Scan(&result.Trend); err != nil {
		return result, gerror.Wrap(err, "load usage trend")
	}
	if err := base.Clone().Fields(`requested_model AS name,COUNT(*) AS requests,COALESCE(SUM(total_tokens),0) AS total_tokens,COALESCE(SUM(estimated_cost),0) AS estimated_cost`).
		Group(dao.UsageLogs.Columns().RequestedModel).OrderDesc("requests").Limit(8).Scan(&result.ByModel); err != nil {
		return result, gerror.Wrap(err, "load model breakdown")
	}
	if err := dao.UsageLogs.Ctx(ctx).As("u").WhereGTE("u."+dao.UsageLogs.Columns().CreatedAt, start).
		Fields(`COALESCE(c.name,'不可用渠道') AS name,COUNT(*) AS requests,COALESCE(SUM(u.total_tokens),0) AS total_tokens,COALESCE(SUM(u.estimated_cost),0) AS estimated_cost`).
		LeftJoin(dao.Channels.Table()+" c", "c.id=u.channel_id").Group("u.channel_id,c.name").OrderDesc("requests").Limit(8).Scan(&result.ByChannel); err != nil {
		return result, gerror.Wrap(err, "load channel breakdown")
	}
	recentCost, err := s.recentCostDistribution(ctx, now)
	if err != nil {
		return result, err
	}
	result.RecentCost = recentCost
	return result, nil
}

func (s *Service) recentCostDistribution(ctx context.Context, now time.Time) (RecentCostDistribution, error) {
	start := now.Add(-time.Duration(recentCostHours-1) * time.Hour).Truncate(time.Hour)
	result := RecentCostDistribution{Models: make([]RecentCostModel, 0)}
	base := dao.UsageLogs.Ctx(ctx).WhereGTE(dao.UsageLogs.Columns().CreatedAt, start)

	var total struct {
		EstimatedCost float64 `orm:"estimated_cost"`
	}
	if err := base.Clone().Fields("COALESCE(SUM(estimated_cost),0) AS estimated_cost").Scan(&total); err != nil {
		return result, gerror.Wrap(err, "load recent cost total")
	}
	result.TotalEstimatedCost = total.EstimatedCost

	var models []struct {
		Name          string  `orm:"name"`
		EstimatedCost float64 `orm:"estimated_cost"`
	}
	if err := base.Clone().Fields("requested_model AS name, COALESCE(SUM(estimated_cost),0) AS estimated_cost").
		Group(dao.UsageLogs.Columns().RequestedModel).OrderDesc("estimated_cost").OrderAsc(dao.UsageLogs.Columns().RequestedModel).
		Limit(recentCostModelLimit).Scan(&models); err != nil {
		return result, gerror.Wrap(err, "load recent cost models")
	}
	if len(models) == 0 {
		return result, nil
	}

	selectedNames := make(map[string]struct{}, len(models))
	for _, model := range models {
		selectedNames[model.Name] = struct{}{}
	}
	var rows []struct {
		Bucket        string  `orm:"bucket"`
		Name          string  `orm:"name"`
		EstimatedCost float64 `orm:"estimated_cost"`
	}
	if err := base.Clone().
		Fields("DATE_FORMAT(created_at,'%Y-%m-%d %H:00:00') AS bucket, requested_model AS name, COALESCE(SUM(estimated_cost),0) AS estimated_cost").
		Group("bucket, requested_model").OrderAsc("bucket").Scan(&rows); err != nil {
		return result, gerror.Wrap(err, "load hourly recent costs")
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
		result.Models = append(result.Models, RecentCostModel{Name: model.Name, Points: hourlyCostPoints(start, costsByModel[model.Name])})
	}
	if hasOtherModels {
		result.Models = append(result.Models, RecentCostModel{Name: otherCostModelName, Points: hourlyCostPoints(start, costsByModel[otherCostModelName])})
	}
	return result, nil
}

func hourlyCostPoints(start time.Time, costs map[string]float64) []HourlyCostPoint {
	points := make([]HourlyCostPoint, 0, recentCostHours)
	for offset := 0; offset < recentCostHours; offset++ {
		hour := start.Add(time.Duration(offset) * time.Hour)
		bucket := hour.Format(hourBucketLayout)
		points = append(points, HourlyCostPoint{Bucket: bucket, EstimatedCost: costs[bucket]})
	}
	return points
}

func (s *Service) UserSummary(ctx context.Context, userID uint64, days int) (UserSummary, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	result := UserSummary{Days: days}
	start := time.Now().AddDate(0, 0, -days+1).Truncate(24 * time.Hour)
	err := dao.UsageLogs.Ctx(ctx).
		Where(dao.UsageLogs.Columns().UserId, userID).
		WhereGTE(dao.UsageLogs.Columns().CreatedAt, start).
		Fields(`
			COUNT(*) AS requests,
			COALESCE(SUM(CASE WHEN http_status BETWEEN 200 AND 299 THEN 1 ELSE 0 END),0) AS successes,
			COALESCE(SUM(input_tokens),0) AS input_tokens,
			COALESCE(SUM(output_tokens),0) AS output_tokens,
			COALESCE(SUM(total_tokens),0) AS total_tokens,
			COALESCE(SUM(estimated_cost),0) AS estimated_cost`).
		Scan(&result)
	return result, gerror.Wrap(err, "load user usage summary")
}

func (s *Service) List(ctx context.Context, input LogFilter) (LogPage, error) {
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		input.PageSize = 20
	}
	if input.EndAt.Before(input.StartAt) {
		return LogPage{}, gerror.New("结束时间不能早于开始时间")
	}
	query := dao.UsageLogs.Ctx(ctx).As("u")
	query = query.WhereGTE("u.created_at", input.StartAt).WhereLTE("u.created_at", input.EndAt)
	if input.ModelName != "" {
		query = query.WhereLike("u.requested_model", "%"+input.ModelName+"%")
	}
	if input.ChannelID > 0 {
		query = query.Where("u.channel_id", input.ChannelID)
	}
	if input.APIKeyID > 0 {
		query = query.Where("u.api_key_id", input.APIKeyID)
	}
	if input.UserID > 0 {
		query = query.Where("u.user_id", input.UserID)
	}
	var summary LogSummary
	if err := query.Clone().Fields("COUNT(*) AS requests,COALESCE(SUM(u.estimated_cost),0) AS estimated_cost").Scan(&summary); err != nil {
		return LogPage{}, gerror.Wrap(err, "count usage logs")
	}
	items := make([]LogView, 0)
	err := query.Fields("u.*,COALESCE(k.name,'系统测试') AS api_key_name,c.name AS channel_name,IF(u.api_key_id IS NULL,'系统',COALESCE(usr.name,'已删除用户')) AS user_name").
		LeftJoin(dao.ApiKeys.Table()+" k", "k.id=u.api_key_id").
		LeftJoin(dao.Channels.Table()+" c", "c.id=u.channel_id").
		LeftJoin(dao.Users.Table()+" usr", "usr.id=u.user_id").
		OrderDesc("u.id").Page(input.Page, input.PageSize).Scan(&items)
	if err != nil {
		return LogPage{}, gerror.Wrap(err, "list usage logs")
	}
	for index := range items {
		items[index].BillingDetails = ParseBillingBreakdown(items[index].BillingDetailsJSON)
	}
	s.reconstructLegacyBillingDetails(ctx, items)
	s.populateIPLocations(items)
	return LogPage{Items: items, Summary: summary, StartAt: input.StartAt, EndAt: input.EndAt, Total: int(summary.Requests), Page: input.Page, PageSize: input.PageSize}, nil
}
