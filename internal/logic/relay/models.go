package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
	"sort"
	"time"
)

// modelsListCacheKey 复用历史键名 aiferry:models:list 并嵌入路由版本号：
// 所有渠道/模型/分组写路径都会递增 aiferry:routes:version，版本号变化后
// 旧列表键自然失效。短 TTL 兜底防止版本号丢失。
const modelsListCacheTTL = 60 * time.Second

func (s *sRelay) Models(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
	version := s.routeCacheVersion(ctx)
	cacheKey := fmt.Sprintf("aiferry:models:list:metadata-v1:%d:%d", key.Id, version)
	if cached, err := s.app.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var list ModelList
		if json.Unmarshal(cached, &list) == nil {
			return list, nil
		}
	}
	list, err := s.computeModels(ctx, key)
	if err != nil {
		return ModelList{}, err
	}
	if encoded, err := json.Marshal(list); err == nil {
		_ = s.app.Redis.Set(ctx, cacheKey, encoded, modelsListCacheTTL).Err()
	}
	return list, nil
}

// computeModels 逐模型解析可用渠道候选，得出该密钥可见的模型列表。
// routeCached 命中缓存时每个模型只消耗 1 次 Redis 读，无数据库查询。
func (s *sRelay) computeModels(ctx context.Context, key apikey.AuthKey) (ModelList, error) {
	modelColumns := dao.ChannelModels.Columns()
	rows := make([]struct {
		ChannelId  uint64 `orm:"channel_id"`
		PublicName string `orm:"public_name"`
	}, 0)
	err := dao.ChannelModels.Ctx(ctx).
		Fields(modelColumns.ChannelId, modelColumns.PublicName).
		Where(modelColumns.Enabled, 1).
		WhereNull(modelColumns.AutoDisabledAt).
		Scan(&rows)
	if err != nil {
		return ModelList{}, gerror.Wrap(err, "list public models")
	}
	channelIDs := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		channelIDs[row.ChannelId] = struct{}{}
	}
	activeChannels, err := activeRouteChannels(ctx, sortedRouteIDs(channelIDs))
	if err != nil {
		return ModelList{}, err
	}
	publicNames := make(map[string]struct{})
	for _, row := range rows {
		if _, active := activeChannels[row.ChannelId]; active {
			publicNames[row.PublicName] = struct{}{}
		}
	}
	names := make([]string, 0, len(publicNames))
	for name := range publicNames {
		names = append(names, name)
	}
	sort.Strings(names)
	catalog, err := modelmetadata.Load(ctx, sortedRouteIDs(channelIDs), names)
	if err != nil {
		return ModelList{}, err
	}
	models := make([]Model, 0, len(names))
	for _, name := range names {
		if len(key.AllowedModels) > 0 && !containsString(key.AllowedModels, name) {
			continue
		}
		candidates, routeErr := s.routeCached(ctx, name, key)
		if routeErr != nil {
			return ModelList{}, routeErr
		}
		if len(candidates) > 0 {
			refs := make([]modelmetadata.Ref, 0, len(candidates))
			for _, candidate := range candidates {
				refs = append(refs, modelmetadata.Ref{ChannelID: candidate.ChannelID, ModelName: candidate.UpstreamName})
			}
			metadata := catalog.Resolve(name, refs).Effective
			models = append(models, Model{ID: name, Object: "model", Created: 0, OwnedBy: "aiferry", Metadata: metadata})
		}
	}
	return ModelList{Object: "list", Data: models}, nil
}
