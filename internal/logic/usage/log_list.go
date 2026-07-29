package usage

import (
	"context"
	"sort"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sUsage) listUsageLogs(ctx context.Context, input LogFilter) (LogPage, error) {
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		input.PageSize = 20
	}
	if input.EndAt.Before(input.StartAt) {
		return LogPage{}, gerror.New("结束时间不能早于开始时间")
	}
	query := usageLogQuery(ctx, input)
	total, err := query.Clone().Count()
	if err != nil {
		return LogPage{}, gerror.Wrap(err, "count usage logs")
	}
	columns := dao.UsageLogs.Columns()
	costs := make([]struct {
		EstimatedCost float64 `orm:"estimated_cost"`
	}, 0)
	if err = query.Clone().Fields(columns.EstimatedCost).Scan(&costs); err != nil {
		return LogPage{}, gerror.Wrap(err, "load usage log costs")
	}
	summary := LogSummary{Requests: int64(total)}
	for _, cost := range costs {
		summary.EstimatedCost += cost.EstimatedCost
	}
	items := make([]LogView, 0)
	if err = query.OrderDesc(columns.Id).Page(input.Page, input.PageSize).Scan(&items); err != nil {
		return LogPage{}, gerror.Wrap(err, "list usage logs")
	}
	if err = populateUsageLogReferences(ctx, items); err != nil {
		return LogPage{}, err
	}
	for index := range items {
		items[index].BillingDetails = ParseBillingBreakdown(items[index].BillingDetailsJSON)
	}
	s.reconstructLegacyBillingDetails(ctx, items)
	s.populateIPLocations(items)
	return LogPage{Items: items, Summary: summary, StartAt: input.StartAt, EndAt: input.EndAt, Total: total, Page: input.Page, PageSize: input.PageSize}, nil
}

func usageLogQuery(ctx context.Context, input LogFilter) *gdb.Model {
	columns := dao.UsageLogs.Columns()
	query := dao.UsageLogs.Ctx(ctx).
		WhereGTE(columns.CreatedAt, input.StartAt).
		WhereLTE(columns.CreatedAt, input.EndAt)
	if input.ModelName != "" {
		query = query.WhereLike(columns.RequestedModel, "%"+input.ModelName+"%")
	}
	if input.ChannelID > 0 {
		query = query.Where(columns.ChannelId, input.ChannelID)
	}
	if input.APIKeyID > 0 {
		query = query.Where(columns.ApiKeyId, input.APIKeyID)
	}
	if input.UserID > 0 {
		query = query.Where(columns.UserId, input.UserID)
	}
	return query
}

func loadUsageChannelNames(ctx context.Context, channelIDs []uint64) (map[uint64]string, error) {
	result := make(map[uint64]string)
	if len(channelIDs) == 0 {
		return result, nil
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0, len(channelIDs))
	if err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.Name).
		WhereIn(columns.Id, channelIDs).
		Scan(&channels); err != nil {
		return nil, gerror.Wrap(err, "load usage log channels")
	}
	for _, channel := range channels {
		result[channel.Id] = channel.Name
	}
	return result, nil
}

func populateUsageLogReferences(ctx context.Context, items []LogView) error {
	if len(items) == 0 {
		return nil
	}
	channelIDSet := make(map[uint64]struct{})
	apiKeyIDSet := make(map[uint64]struct{})
	userIDSet := make(map[uint64]struct{})
	for _, item := range items {
		if item.ChannelId > 0 {
			channelIDSet[item.ChannelId] = struct{}{}
		}
		if item.APIKeyId > 0 {
			apiKeyIDSet[item.APIKeyId] = struct{}{}
		}
		if item.UserId > 0 {
			userIDSet[item.UserId] = struct{}{}
		}
	}
	channelIDs := usageReferenceIDs(channelIDSet)
	channelNames, err := loadUsageChannelNames(ctx, channelIDs)
	if err != nil {
		return err
	}
	credentialIndexes, err := loadUsageCredentialIndexes(ctx, channelIDs)
	if err != nil {
		return err
	}
	apiKeyNames, err := loadUsageAPIKeyNames(ctx, usageReferenceIDs(apiKeyIDSet))
	if err != nil {
		return err
	}
	userNames, err := loadUsageUserNames(ctx, usageReferenceIDs(userIDSet))
	if err != nil {
		return err
	}
	for index := range items {
		item := &items[index]
		item.ChannelName = channelNames[item.ChannelId]
		if item.ChannelName == "" {
			item.ChannelName = "已删除渠道"
		}
		item.ChannelCredentialIndex = credentialIndexes[item.ChannelCredentialId]
		item.APIKeyName = apiKeyNames[item.APIKeyId]
		if item.APIKeyName == "" {
			item.APIKeyName = "系统测试"
		}
		if item.APIKeyId == 0 {
			item.UserName = "系统"
		} else {
			item.UserName = userNames[item.UserId]
			if item.UserName == "" {
				item.UserName = "已删除用户"
			}
		}
	}
	return nil
}

func loadUsageCredentialIndexes(ctx context.Context, channelIDs []uint64) (map[uint64]uint, error) {
	indexes := make(map[uint64]uint)
	if len(channelIDs) == 0 {
		return indexes, nil
	}
	columns := dao.ChannelCredentials.Columns()
	credentials := make([]entity.ChannelCredentials, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).Unscoped().
		Fields(columns.Id, columns.ChannelId).
		WhereIn(columns.ChannelId, channelIDs).
		OrderAsc(columns.ChannelId).
		OrderAsc(columns.Id).
		Scan(&credentials); err != nil {
		return nil, gerror.Wrap(err, "load usage log channel credentials")
	}
	perChannel := make(map[uint64]uint)
	for _, credential := range credentials {
		perChannel[credential.ChannelId]++
		indexes[credential.Id] = perChannel[credential.ChannelId]
	}
	return indexes, nil
}

func loadUsageAPIKeyNames(ctx context.Context, apiKeyIDs []uint64) (map[uint64]string, error) {
	result := make(map[uint64]string)
	if len(apiKeyIDs) == 0 {
		return result, nil
	}
	columns := dao.ApiKeys.Columns()
	keys := make([]entity.ApiKeys, 0, len(apiKeyIDs))
	if err := dao.ApiKeys.Ctx(ctx).
		Fields(columns.Id, columns.Name).
		WhereIn(columns.Id, apiKeyIDs).
		Scan(&keys); err != nil {
		return nil, gerror.Wrap(err, "load usage log API keys")
	}
	for _, key := range keys {
		result[key.Id] = key.Name
	}
	return result, nil
}

func loadUsageUserNames(ctx context.Context, userIDs []uint64) (map[uint64]string, error) {
	result := make(map[uint64]string)
	if len(userIDs) == 0 {
		return result, nil
	}
	columns := dao.Users.Columns()
	users := make([]entity.Users, 0, len(userIDs))
	if err := dao.Users.Ctx(ctx).
		Fields(columns.Id, columns.Name).
		WhereIn(columns.Id, userIDs).
		Scan(&users); err != nil {
		return nil, gerror.Wrap(err, "load usage log users")
	}
	for _, user := range users {
		result[user.Id] = user.Name
	}
	return result, nil
}

func usageChannelIDs(rows []entity.UsageLogs) []uint64 {
	ids := make(map[uint64]struct{})
	for _, row := range rows {
		if row.ChannelId > 0 {
			ids[row.ChannelId] = struct{}{}
		}
	}
	return usageReferenceIDs(ids)
}

func usageReferenceIDs(ids map[uint64]struct{}) []uint64 {
	result := make([]uint64, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
