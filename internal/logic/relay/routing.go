package relay

import (
	"context"
	mathrand "math/rand/v2"
	"net/http"
	"sort"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sRelay) route(ctx context.Context, model string, key apikey.AuthKey) ([]Candidate, error) {
	modelColumns := dao.ChannelModels.Columns()
	models := make([]entity.ChannelModels, 0)
	if err := dao.ChannelModels.Ctx(ctx).
		Where(modelColumns.Enabled, 1).
		Where(modelColumns.PublicName, model).
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
		candidates = append(candidates, Candidate{
			ChannelModelID: row.Id,
			ChannelID:      channel.Id,
			ChannelName:    channel.Name,
			BaseURL:        channel.BaseUrl,
			OrganizationID: channel.OrganizationId,
			ProjectID:      channel.ProjectId,
			ProxyURLCipher: channel.ProxyUrlCipher,
			AdvancedConfig: channel.AdvancedConfig,
			Priority:       channel.Priority,
			Weight:         channel.Weight,
			PublicName:     row.PublicName,
			UpstreamName:   row.UpstreamName,
		})
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

func (s *sRelay) channelGroupIDs(ctx context.Context, channelID uint64) ([]uint64, error) {
	membershipColumns := dao.ChannelGroupMembers.Columns()
	var memberships []uint64
	if err := dao.ChannelGroupMembers.Ctx(ctx).
		Fields(membershipColumns.ChannelGroupId).
		Where(membershipColumns.ChannelId, channelID).
		OrderAsc(membershipColumns.ChannelGroupId).
		Scan(&memberships); err != nil {
		return nil, gerror.Wrap(err, "load channel group memberships")
	}
	if len(memberships) == 0 {
		return nil, nil
	}
	groupColumns := dao.ChannelGroups.Columns()
	groups := make([]entity.ChannelGroups, 0, len(memberships))
	if err := dao.ChannelGroups.Ctx(ctx).
		Fields(groupColumns.Id).
		WhereIn(groupColumns.Id, memberships).
		Where(groupColumns.Status, 1).
		OrderAsc(groupColumns.Id).
		Scan(&groups); err != nil {
		return nil, gerror.Wrap(err, "load active channel groups")
	}
	result := make([]uint64, 0, len(groups))
	for _, group := range groups {
		result = append(result, group.Id)
	}
	return result, nil
}

func activeRouteChannels(ctx context.Context, channelIDs []uint64) (map[uint64]entity.Channels, error) {
	result := make(map[uint64]entity.Channels)
	if len(channelIDs) == 0 {
		return result, nil
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0, len(channelIDs))
	if err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.Name, columns.BaseUrl, columns.OrganizationId, columns.ProjectId, columns.ProxyUrlCipher, columns.AdvancedConfig, columns.Priority, columns.Weight).
		WhereIn(columns.Id, channelIDs).
		Where(columns.Status, 1).
		Scan(&channels); err != nil {
		return nil, gerror.Wrap(err, "load active route channels")
	}
	for _, channel := range channels {
		result[channel.Id] = channel
	}
	return result, nil
}

func sortedRouteIDs(values map[uint64]struct{}) []uint64 {
	result := make([]uint64, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
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

func (s *sRelay) maybeAutoDisable(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, candidate Candidate, result attemptResult) {
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
	if status == http.StatusPaymentRequired {
		return true
	}
	return system.MatchesStatusCodeRules(rules, status)
}
