package system

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/consts"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	AutoDisableSourceRelayRequest = "relay_request"
	AutoDisableSourceModelTest    = "model_test"
	AutoDisableSourceCostQuery    = "cost_query"
	autoDisableSourceUnknown      = "unknown"
)

type AutoDisableInput struct {
	ChannelID           uint64
	ChannelCredentialID uint64
	Source              string
	Status              int
	Latency             time.Duration
	Message             string
	TimedOut            bool
}

func matchesAutoDisable(settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) bool {
	if input.TimedOut {
		return true
	}
	if input.Status > 0 && MatchesStatusCodeRules(settings.DisableStatusCodes, input.Status) {
		return true
	}
	if input.Latency >= time.Duration(settings.DisableLatencySeconds)*time.Second {
		return true
	}
	message := strings.ToLower(input.Message)
	for _, keyword := range settings.FailureKeywords {
		if strings.Contains(message, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func IsAutoDisableMatch(settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) bool {
	return settings.AutoDisableEnabled && matchesAutoDisable(settings, input)
}

func autoDisableReason(input AutoDisableInput) string {
	parts := make([]string, 0, 4)
	if input.Status > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", input.Status))
	}
	if input.Latency > 0 {
		parts = append(parts, "latency="+input.Latency.Round(time.Millisecond).String())
	}
	if input.TimedOut {
		parts = append(parts, "timed_out=true")
	}
	if message := strings.TrimSpace(input.Message); message != "" {
		parts = append(parts, message)
	}
	return truncate(strings.Join(parts, ", "), 1024)
}

func autoDisableSource(source string) string {
	switch source {
	case AutoDisableSourceRelayRequest, AutoDisableSourceModelTest, AutoDisableSourceCostQuery:
		return source
	default:
		return autoDisableSourceUnknown
	}
}

func (s *Service) DisableIfNeeded(ctx context.Context, input AutoDisableInput) (bool, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return false, err
	}
	return s.DisableIfNeededWithSettings(ctx, settings, input)
}

func (s *Service) DisableIfNeededWithSettings(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) (bool, error) {
	if !IsAutoDisableMatch(settings, input) {
		return false, nil
	}
	var channel entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: input.ChannelID}).Scan(&channel); err != nil {
		return false, gerror.Wrap(err, "load channel for automatic disable")
	}
	if channel.Id == 0 {
		return false, gerror.New("channel not found")
	}
	if channel.AutoDisableEnabled != 1 || channel.Status == 0 {
		return false, nil
	}
	if input.ChannelCredentialID > 0 {
		return s.disableCredential(ctx, settings, input)
	}
	matched, err := s.recordAutoDisableFailure(ctx, input.ChannelID, 0, settings.AutoDisableFailureThreshold)
	if err != nil {
		return false, err
	}
	if !matched {
		return false, nil
	}

	data := do.Channels{
		Status:                 0,
		AutoDisabledAt:         gtime.Now(),
		AutoDisabledReason:     autoDisableReason(input),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     autoDisableSource(input.Source),
	}
	if input.Status > 0 {
		data.AutoDisabledStatusCode = input.Status
	}
	result, err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: input.ChannelID, Status: 1}).Data(data).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically disable channel")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.resetRecoverySchedule(ctx, RecoveryTargetChannel, input.ChannelID)
	s.clearTransient(ctx, input.ChannelID)
	return true, nil
}

func (s *Service) disableCredential(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) (bool, error) {
	var credential entity.ChannelCredentials
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: input.ChannelCredentialID, ChannelId: input.ChannelID}).Scan(&credential); err != nil {
		return false, gerror.Wrap(err, "load channel credential for automatic disable")
	}
	if credential.Id == 0 || credential.Status == 0 {
		return false, nil
	}
	matched, err := s.recordAutoDisableFailure(ctx, input.ChannelID, credential.Id, settings.AutoDisableFailureThreshold)
	if err != nil {
		return false, err
	}
	if !matched {
		return false, nil
	}
	data := do.ChannelCredentials{
		Status:                 0,
		AutoDisabledAt:         gtime.Now(),
		AutoDisabledReason:     autoDisableReason(input),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     autoDisableSource(input.Source),
	}
	if input.Status > 0 {
		data.AutoDisabledStatusCode = input.Status
	}
	result, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credential.Id, Status: 1}).Data(data).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically disable channel credential")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.resetRecoverySchedule(ctx, RecoveryTargetCredential, credential.Id)
	s.clearCredentialTransient(ctx, credential.Id)
	return true, nil
}

func (s *Service) RecoverIfAllowed(ctx context.Context, channelID uint64) (bool, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return false, err
	}
	if !settings.RecoveryEnabled {
		return false, nil
	}
	var channel entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID}).Scan(&channel); err != nil {
		return false, gerror.Wrap(err, "load channel for automatic recovery")
	}
	if channel.Id == 0 {
		return false, gerror.New("channel not found")
	}
	if channel.Status != 0 || channel.AutoDisabledAt.IsZero() {
		return false, nil
	}
	result, err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID, Status: 0}).Data(do.Channels{
		Status:                 1,
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically recover channel")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearRecoverySchedule(ctx, RecoveryTargetChannel, channelID)
	s.clearTransient(ctx, channelID)
	return true, nil
}

func (s *Service) RecoverCredentialIfAllowed(ctx context.Context, credentialID uint64) (bool, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return false, err
	}
	if !settings.RecoveryEnabled {
		return false, nil
	}
	var credential entity.ChannelCredentials
	if err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID}).Scan(&credential); err != nil {
		return false, gerror.Wrap(err, "load channel credential for automatic recovery")
	}
	if credential.Id == 0 || credential.Status != 0 || credential.AutoDisabledAt.IsZero() {
		return false, nil
	}
	result, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credential.Id, Status: 0}).Data(do.ChannelCredentials{
		Status:                 1,
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically recover channel credential")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearRecoverySchedule(ctx, RecoveryTargetCredential, credential.Id)
	s.clearCredentialTransient(ctx, credential.Id)
	return true, nil
}

func (s *Service) clearTransient(ctx context.Context, channelID uint64) {
	_ = s.app.Redis.Del(ctx, failureKey(channelID), cooldownKey(channelID), consts.ChannelListCacheKey).Err()
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}

func (s *Service) clearCredentialTransient(ctx context.Context, credentialID uint64) {
	_ = s.app.Redis.Del(ctx,
		CredentialFailureKey(credentialID),
		CredentialCooldownKey(credentialID),
		consts.ChannelListCacheKey,
	).Err()
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}
