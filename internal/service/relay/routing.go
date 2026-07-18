package relay

import (
	"context"
	mathrand "math/rand/v2"
	"sort"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/channel"
	"github.com/yunloli/aiferry/internal/service/system"
)

func (s *Service) route(ctx context.Context, model string, key apikey.AuthKey) ([]Candidate, error) {
	var candidates []Candidate
	err := dao.ChannelModels.Ctx(ctx).As("m").Fields(`
		m.id AS channel_model_id,m.public_name,m.upstream_name,
		c.id AS channel_id,c.name AS channel_name,c.base_url,
		c.organization_id,c.project_id,c.proxy_url_cipher,c.advanced_config,c.priority,c.weight`).
		InnerJoin(dao.Channels.Table()+" c", "c.id=m.channel_id AND c.status=1").
		Where("m.enabled", 1).
		Where("m.public_name", model).
		Scan(&candidates)
	if err != nil {
		return nil, gerror.Wrap(err, "load model routes")
	}
	available := candidates[:0]
	for _, candidate := range candidates {
		groupIDs, groupErr := s.channelGroupIDs(ctx, candidate.ChannelID)
		if groupErr != nil {
			return nil, groupErr
		}
		if !keyAllowsGroups(key, groupIDs) {
			continue
		}
		hasCredential, credentialErr := s.channels.HasAvailableCredential(ctx, candidate.ChannelID)
		if credentialErr != nil {
			return nil, credentialErr
		}
		if !hasCredential {
			continue
		}
		candidate.GroupIDs = groupIDs
		available = append(available, candidate)
	}
	return weightedOrder(available), nil
}

func weightedOrder(candidates []Candidate) []Candidate {
	groups := make(map[int][]Candidate)
	priorities := make([]int, 0)
	for _, candidate := range candidates {
		if _, ok := groups[candidate.Priority]; !ok {
			priorities = append(priorities, candidate.Priority)
		}
		groups[candidate.Priority] = append(groups[candidate.Priority], candidate)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))
	ordered := make([]Candidate, 0, len(candidates))
	for _, priority := range priorities {
		pool := append([]Candidate(nil), groups[priority]...)
		for len(pool) > 0 {
			total := uint64(0)
			for _, item := range pool {
				total += uint64(max(item.Weight, 1))
			}
			pick := mathrand.Uint64N(total)
			selected := 0
			for index, item := range pool {
				weight := uint64(max(item.Weight, 1))
				if pick < weight {
					selected = index
					break
				}
				pick -= weight
			}
			ordered = append(ordered, pool[selected])
			pool = append(pool[:selected], pool[selected+1:]...)
		}
	}
	return ordered
}

func (s *Service) channelGroupIDs(ctx context.Context, channelID uint64) ([]uint64, error) {
	var ids []uint64
	err := dao.ChannelGroupMembers.Ctx(ctx).As("m").
		Fields("m.channel_group_id").
		InnerJoin(dao.ChannelGroups.Table()+" g", "g.id=m.channel_group_id AND g.status=1").
		Where("m.channel_id", channelID).
		OrderAsc("m.channel_group_id").
		Scan(&ids)
	return ids, gerror.Wrap(err, "load channel groups")
}

func keyAllowsModel(key apikey.AuthKey, model string) bool {
	return len(key.AllowedModels) == 0 || containsString(key.AllowedModels, model)
}
func keyAllowsGroups(key apikey.AuthKey, groupIDs []uint64) bool {
	if len(key.ChannelGroupIDs) == 0 {
		return true
	}
	for _, groupID := range groupIDs {
		for _, allowed := range key.ChannelGroupIDs {
			if allowed == groupID {
				return true
			}
		}
	}
	return false
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) markFailure(ctx context.Context, credentialID uint64) {
	key := channel.CredentialFailureKey(credentialID)
	count, err := s.app.Redis.Incr(ctx, key).Result()
	if err != nil {
		return
	}
	_ = s.app.Redis.Expire(ctx, key, 10*time.Minute).Err()
	if count >= s.app.Config.FailureThreshold {
		_ = s.app.Redis.Set(ctx, channel.CredentialCooldownKey(credentialID), "1", time.Duration(s.app.Config.ChannelCooldownSeconds)*time.Second).Err()
	}
}

func (s *Service) markSuccess(ctx context.Context, credentialID uint64) {
	_ = s.app.Redis.Del(ctx, channel.CredentialFailureKey(credentialID), channel.CredentialCooldownKey(credentialID)).Err()
}

func (s *Service) maybeAutoDisable(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, candidate Candidate, result attemptResult) {
	_, _ = s.resilience.DisableIfNeededWithSettings(ctx, settings, system.AutoDisableInput{
		ChannelID: candidate.ChannelID, ChannelCredentialID: candidate.ChannelCredentialID,
		Source:   system.AutoDisableSourceRelayRequest,
		Status:   result.status,
		Latency:  result.latency,
		Message:  result.errorMessage,
		TimedOut: result.timedOut,
	})
}

func retryableStatus(status int) bool {
	return retryableStatusForRules(status, system.DefaultResilienceSettings().RetryStatusCodes)
}

func retryableStatusForRules(status int, rules string) bool {
	return system.MatchesStatusCodeRules(rules, status)
}
