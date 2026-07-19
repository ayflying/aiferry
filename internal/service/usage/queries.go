package usage

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
)

func (s *Service) Dashboard(ctx context.Context, days int) (Dashboard, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	start := time.Now().AddDate(0, 0, -days+1).Truncate(24 * time.Hour)
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
	return result, nil
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

func (s *Service) List(ctx context.Context, page, pageSize int, modelName string, channelID, apiKeyID, userID uint64) (LogPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := dao.UsageLogs.Ctx(ctx).As("u")
	if modelName != "" {
		query = query.WhereLike("u.requested_model", "%"+modelName+"%")
	}
	if channelID > 0 {
		query = query.Where("u.channel_id", channelID)
	}
	if apiKeyID > 0 {
		query = query.Where("u.api_key_id", apiKeyID)
	}
	if userID > 0 {
		query = query.Where("u.user_id", userID)
	}
	total, err := query.Clone().Count()
	if err != nil {
		return LogPage{}, gerror.Wrap(err, "count usage logs")
	}
	items := make([]LogView, 0)
	err = query.Fields("u.*,COALESCE(k.name,'系统测试') AS api_key_name,c.name AS channel_name,IF(u.api_key_id IS NULL,'系统',COALESCE(usr.name,'已删除用户')) AS user_name").
		LeftJoin(dao.ApiKeys.Table()+" k", "k.id=u.api_key_id").
		LeftJoin(dao.Channels.Table()+" c", "c.id=u.channel_id").
		LeftJoin(dao.Users.Table()+" usr", "usr.id=u.user_id").
		OrderDesc("u.id").Page(page, pageSize).Scan(&items)
	return LogPage{Items: items, Total: total, Page: page, PageSize: pageSize}, gerror.Wrap(err, "list usage logs")
}
