package apikey

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *Service) replacePolicy(ctx context.Context, keyID uint64, models []string, groupIDs []uint64) error {
	if _, err := dao.ApiKeyModels.Ctx(ctx).Where(dao.ApiKeyModels.Columns().ApiKeyId, keyID).Delete(); err != nil {
		return gerror.Wrap(err, "clear key model policy")
	}
	if _, err := dao.ApiKeyChannelGroups.Ctx(ctx).Where(dao.ApiKeyChannelGroups.Columns().ApiKeyId, keyID).Delete(); err != nil {
		return gerror.Wrap(err, "clear key group policy")
	}
	for _, model := range normalizeModels(models) {
		if _, err := dao.ApiKeyModels.Ctx(ctx).Data(do.ApiKeyModels{ApiKeyId: keyID, ModelName: model}).Insert(); err != nil {
			return gerror.Wrap(err, "save key model policy")
		}
	}
	for _, groupID := range uniqueIDs(groupIDs) {
		if _, err := dao.ApiKeyChannelGroups.Ctx(ctx).Data(do.ApiKeyChannelGroups{ApiKeyId: keyID, ChannelGroupId: groupID}).Insert(); err != nil {
			return gerror.Wrap(err, "save key group policy")
		}
	}
	return nil
}

func (s *Service) populatePolicy(ctx context.Context, view *View) error {
	if view.SpendLimit != nil {
		remaining := *view.SpendLimit - view.SpentAmount
		if remaining < 0 {
			remaining = 0
		}
		view.AvailableAmount = &remaining
	}
	var err error
	if view.AllowedModels, err = listModels(ctx, view.Id); err != nil {
		return err
	}
	if view.ChannelGroupIDs, err = listGroupIDs(ctx, view.Id); err != nil {
		return err
	}
	return nil
}

func (s *Service) populateAuthPolicy(ctx context.Context, key *AuthKey) error {
	var err error
	if key.AllowedModels, err = listModels(ctx, key.Id); err != nil {
		return err
	}
	key.ChannelGroupIDs, err = listGroupIDs(ctx, key.Id)
	return err
}

func listModels(ctx context.Context, keyID uint64) ([]string, error) {
	models := make([]string, 0)
	err := dao.ApiKeyModels.Ctx(ctx).Fields(dao.ApiKeyModels.Columns().ModelName).Where(dao.ApiKeyModels.Columns().ApiKeyId, keyID).OrderAsc(dao.ApiKeyModels.Columns().ModelName).Scan(&models)
	return models, gerror.Wrap(err, "list key model policy")
}

func listGroupIDs(ctx context.Context, keyID uint64) ([]uint64, error) {
	ids := make([]uint64, 0)
	err := dao.ApiKeyChannelGroups.Ctx(ctx).Fields(dao.ApiKeyChannelGroups.Columns().ChannelGroupId).Where(dao.ApiKeyChannelGroups.Columns().ApiKeyId, keyID).OrderAsc(dao.ApiKeyChannelGroups.Columns().ChannelGroupId).Scan(&ids)
	return ids, gerror.Wrap(err, "list key group policy")
}

func normalizeModels(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueIDs(values []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value > 0 {
			seen[value] = struct{}{}
		}
	}
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsID(values []uint64, target uint64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func decimalLiteral(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.8f", value), "0"), ".")
}

func cacheKey(hash string) string {
	return "aiferry:api-key:" + hash
}
