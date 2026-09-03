package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	channelconfig "github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 路由候选缓存：把 route() 的静态解析结果（模型映射 + 渠道 + 分组）缓存到
// Redis，避免每个转发请求重复执行 2+3N 次数据库查询。
//
// 失效策略采用"版本号嵌入缓存键"：aiferry:routes:version 由所有渠道/模型/
// 分组/凭证写路径通过 invalidateRoutes() 递增，版本号变化后旧键自然失效，
// 无需逐键删除。TTL 仅作为兜底，防止版本号递增丢失（如 Redis 重启且未持久化）。
//
// 缓存内容不含动态状态：凭证冷却（Redis TTL key）与加权随机排序仍在每次
// 请求时执行，保证禁用恢复与负载均衡行为不变。
const (
	routeCacheVersionKey = "aiferry:routes:version"
	routeCacheTTL        = 30 * time.Minute
)

// routeCacheEntry 是 route() 静态解析结果的缓存载体。
// 只保存与密钥无关的渠道候选；分组策略过滤在读取后按密钥执行。
type routeCacheEntry struct {
	Candidates []Candidate `json:"candidates"`
}

func routeCacheKey(model string, version int64) string {
	return fmt.Sprintf("aiferry:routes:%s:%d", model, version)
}

// routeCacheVersion 读取当前路由版本号；读取失败视为 0，让请求走数据库
// 路径并在下次写入时恢复缓存，不因 Redis 异常阻断转发。
func (s *sRelay) routeCacheVersion(ctx context.Context) int64 {
	version, err := s.app.Redis.Get(ctx, routeCacheVersionKey).Int64()
	if err != nil {
		return 0
	}
	return version
}

func (s *sRelay) readRouteCache(ctx context.Context, model string, version int64) ([]Candidate, bool) {
	if s.app == nil || s.app.Redis == nil {
		return nil, false
	}
	encoded, err := s.app.Redis.Get(ctx, routeCacheKey(model, version)).Bytes()
	if err != nil {
		return nil, false
	}
	var entry routeCacheEntry
	if err := json.Unmarshal(encoded, &entry); err != nil || entry.Candidates == nil {
		_ = s.app.Redis.Del(ctx, routeCacheKey(model, version)).Err()
		return nil, false
	}
	return entry.Candidates, true
}

func (s *sRelay) writeRouteCache(ctx context.Context, model string, version int64, candidates []Candidate) {
	if s.app == nil || s.app.Redis == nil || len(candidates) == 0 {
		return
	}
	encoded, err := json.Marshal(routeCacheEntry{Candidates: candidates})
	if err != nil {
		return
	}
	_ = s.app.Redis.Set(ctx, routeCacheKey(model, version), encoded, routeCacheTTL).Err()
}

// routeCached 在 route() 基础上叠加 Redis 缓存：
//  1. 命中缓存时跳过 channel_models / channels / channel_groups 三类静态查询；
//  2. 分组策略过滤与加权排序按密钥实时执行，凭证冷却由选凭证阶段兜底；
//  3. 数据库路径解析完成后回写缓存，供后续请求（含其他密钥）复用。
func (s *sRelay) routeCached(ctx context.Context, model string, key apikey.AuthKey) ([]Candidate, error) {
	version := s.routeCacheVersion(ctx)
	if candidates, ok := s.readRouteCache(ctx, model, version); ok {
		return s.filterCandidatesByKey(ctx, candidates, key)
	}
	candidates, err := s.routeStatic(ctx, model)
	if err != nil {
		return nil, err
	}
	s.writeRouteCache(ctx, model, version, candidates)
	return s.filterCandidatesByKey(ctx, candidates, key)
}

// routeStatic 解析与密钥无关的渠道候选：启用的模型映射 + 激活渠道 + 激活分组。
// 凭证存在性属于渠道静态配置（是否有密钥池），纳入缓存；冷却状态动态排除。
func (s *sRelay) routeStatic(ctx context.Context, model string) ([]Candidate, error) {
	modelColumns := dao.ChannelModels.Columns()
	models := make([]entity.ChannelModels, 0)
	if err := dao.ChannelModels.Ctx(ctx).
		Where(modelColumns.Enabled, 1).
		Where(modelColumns.PublicName, model).
		WhereNull(modelColumns.AutoDisabledAt).
		Scan(&models); err != nil {
		return nil, gerror.Wrap(err, "load model routes")
	}
	channelIDs := make(map[uint64]struct{}, len(models))
	for _, row := range models {
		channelIDs[row.ChannelId] = struct{}{}
	}
	channels, err := activeRouteChannels(ctx, sortedRouteIDs(channelIDs))
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(models))
	for _, row := range models {
		channel, exists := channels[row.ChannelId]
		if !exists {
			continue
		}
		advancedConfig, configErr := channelconfig.ParseAdvancedConfig([]byte(channel.AdvancedConfig))
		if configErr != nil {
			return nil, gerror.Wrap(configErr, "parse channel advanced config")
		}
		groupIDs, groupErr := s.channelGroupIDs(ctx, row.ChannelId)
		if groupErr != nil {
			return nil, groupErr
		}
		hasCredential, credentialErr := s.channels.HasAvailableCredential(ctx, row.ChannelId)
		if credentialErr != nil {
			return nil, credentialErr
		}
		if !hasCredential {
			continue
		}
		candidates = append(candidates, Candidate{
			ChannelModelID: row.Id,
			ChannelID:      channel.Id,
			ChannelName:    channel.Name,
			ChannelType:    channel.Type,
			BaseURL:        channel.BaseUrl,
			BackupBaseURLs: advancedConfig.BackupBaseURLs,
			OrganizationID: channel.OrganizationId,
			ProjectID:      channel.ProjectId,
			ProxyURLCipher: channel.ProxyUrlCipher,
			AdvancedConfig: channel.AdvancedConfig,
			Priority:       channel.Priority,
			Weight:         channel.Weight,
			PublicName:     row.PublicName,
			UpstreamName:   row.UpstreamName,
			GroupIDs:       groupIDs,
		})
	}
	return candidates, nil
}

// filterCandidatesByKey 对缓存候选执行密钥相关过滤与动态排序：
// 分组策略（管理员/用户分组）→ 加权随机排序。凭证冷却在 attemptChannel
// 阶段由 SelectCredential 的排除逻辑兜底。
func (s *sRelay) filterCandidatesByKey(ctx context.Context, candidates []Candidate, key apikey.AuthKey) ([]Candidate, error) {
	var adminRoles []string
	if s.app != nil {
		adminRoles = s.app.Config.AdminRoles
	}
	return filterCandidates(candidates, key, adminRoles), nil
}

// filterCandidates 是分组策略过滤 + 加权排序的纯函数实现，便于单测。
func filterCandidates(candidates []Candidate, key apikey.AuthKey, adminRoles []string) []Candidate {
	// candidates 可能直接来自缓存反序列化的切片，复制一层避免污染缓存对象。
	pool := make([]Candidate, len(candidates))
	copy(pool, candidates)
	isAdmin := false
	for _, role := range adminRoles {
		if role == key.UserRole {
			isAdmin = true
			break
		}
	}
	available := pool[:0]
	for _, candidate := range pool {
		if !keyAllowsGroupPolicy(key, candidate.GroupIDs, isAdmin, key.UserChannelGroupIDs) {
			continue
		}
		available = append(available, candidate)
	}
	return weightedOrder(available)
}
