package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"
)

const (
	autoDisableFailureTTL = 10 * time.Minute
	recoveryLockTTL       = 3 * time.Minute
)

var recoveryRetryIntervals = []time.Duration{
	30 * time.Second,
	time.Minute,
	2 * time.Minute,
	4 * time.Minute,
	5 * time.Minute,
}

type RecoveryTarget string

const (
	RecoveryTargetChannel    RecoveryTarget = "channel"
	RecoveryTargetCredential RecoveryTarget = "credential"
)

type recoveryState struct {
	Attempts      int       `json:"attempts"`
	LastAttemptAt time.Time `json:"lastAttemptAt"`
}

func CredentialFailureKey(credentialID uint64) string {
	return fmt.Sprintf("aiferry:credential:%d:failures", credentialID)
}

func CredentialCooldownKey(credentialID uint64) string {
	return fmt.Sprintf("aiferry:credential:%d:cooldown", credentialID)
}

func RecoveryRetryInterval(completedAttempts int) time.Duration {
	if completedAttempts <= 0 {
		return recoveryRetryIntervals[0]
	}
	if completedAttempts >= len(recoveryRetryIntervals) {
		return recoveryRetryIntervals[len(recoveryRetryIntervals)-1]
	}
	return recoveryRetryIntervals[completedAttempts]
}

func reachesAutoDisableFailureThreshold(count int64, threshold int) bool {
	return count >= int64(threshold)
}

func recoveryStateKey(target RecoveryTarget, id uint64) string {
	return fmt.Sprintf("aiferry:auto-disable-recovery:%s:%d:state", target, id)
}

func recoveryLockKey(target RecoveryTarget, id uint64) string {
	return fmt.Sprintf("aiferry:auto-disable-recovery:%s:%d:lock", target, id)
}

func (s *Service) recordAutoDisableFailure(ctx context.Context, channelID, credentialID uint64, threshold int) (bool, error) {
	if threshold < 1 {
		threshold = DefaultResilienceSettings().AutoDisableFailureThreshold
	}
	if credentialID == 0 {
		return s.recordConsecutiveFailure(ctx, failureKey(channelID), cooldownKey(channelID), threshold)
	}
	return s.recordConsecutiveFailure(ctx, CredentialFailureKey(credentialID), CredentialCooldownKey(credentialID), threshold)
}

func (s *Service) recordConsecutiveFailure(ctx context.Context, counterKey, cooldown string, threshold int) (bool, error) {
	count, err := s.app.Redis.Incr(ctx, counterKey).Result()
	if err != nil {
		return false, gerror.Wrap(err, "increment automatic disable failure count")
	}
	if err = s.app.Redis.Expire(ctx, counterKey, autoDisableFailureTTL).Err(); err != nil {
		return false, gerror.Wrap(err, "expire automatic disable failure count")
	}
	if !reachesAutoDisableFailureThreshold(count, threshold) {
		return false, nil
	}
	if err = s.app.Redis.Set(ctx, cooldown, "1", time.Duration(s.app.Config.ChannelCooldownSeconds)*time.Second).Err(); err != nil {
		return false, gerror.Wrap(err, "set automatic disable cooldown")
	}
	return true, nil
}

func (s *Service) ClearAutoDisableFailures(ctx context.Context, credentialID uint64) {
	if credentialID == 0 {
		return
	}
	_ = s.app.Redis.Del(ctx, CredentialFailureKey(credentialID), CredentialCooldownKey(credentialID)).Err()
}

func (s *Service) ClearChannelAutoDisableFailures(ctx context.Context, channelID uint64) {
	if channelID == 0 {
		return
	}
	_ = s.app.Redis.Del(ctx, failureKey(channelID), cooldownKey(channelID)).Err()
}

func (s *Service) ResetCredentialRecoverySchedule(ctx context.Context, credentialID uint64) {
	s.clearRecoverySchedule(ctx, RecoveryTargetCredential, credentialID)
}

func (s *Service) ResetChannelRecoverySchedule(ctx context.Context, channelID uint64) {
	s.clearRecoverySchedule(ctx, RecoveryTargetChannel, channelID)
}

func (s *Service) BeginRecoveryAttempt(ctx context.Context, target RecoveryTarget, id uint64, autoDisabledAt time.Time) (bool, error) {
	if id == 0 || autoDisabledAt.IsZero() {
		return false, nil
	}
	state, err := s.loadRecoveryState(ctx, target, id)
	if err != nil {
		return false, err
	}
	base := autoDisabledAt
	if !state.LastAttemptAt.IsZero() {
		base = state.LastAttemptAt
	}
	if time.Now().Before(base.Add(RecoveryRetryInterval(state.Attempts))) {
		return false, nil
	}
	locked, err := s.app.Redis.SetNX(ctx, recoveryLockKey(target, id), "1", recoveryLockTTL).Result()
	if err != nil {
		return false, gerror.Wrap(err, "acquire recovery attempt lock")
	}
	return locked, nil
}

func (s *Service) FinishRecoveryAttempt(ctx context.Context, target RecoveryTarget, id uint64, succeeded bool) {
	if succeeded {
		s.clearRecoverySchedule(ctx, target, id)
		return
	}
	state, err := s.loadRecoveryState(ctx, target, id)
	if err == nil {
		state.Attempts++
		state.LastAttemptAt = time.Now()
		if encoded, encodeErr := json.Marshal(state); encodeErr == nil {
			_ = s.app.Redis.Set(ctx, recoveryStateKey(target, id), encoded, 0).Err()
		}
	}
	_ = s.app.Redis.Del(ctx, recoveryLockKey(target, id)).Err()
}

func (s *Service) resetRecoverySchedule(ctx context.Context, target RecoveryTarget, id uint64) {
	s.clearRecoverySchedule(ctx, target, id)
}

func (s *Service) clearRecoverySchedule(ctx context.Context, target RecoveryTarget, id uint64) {
	if id == 0 {
		return
	}
	_ = s.app.Redis.Del(ctx, recoveryStateKey(target, id), recoveryLockKey(target, id)).Err()
}

func (s *Service) loadRecoveryState(ctx context.Context, target RecoveryTarget, id uint64) (recoveryState, error) {
	encoded, err := s.app.Redis.Get(ctx, recoveryStateKey(target, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return recoveryState{}, nil
	}
	if err != nil {
		return recoveryState{}, gerror.Wrap(err, "read recovery attempt state")
	}
	var state recoveryState
	if err = json.Unmarshal(encoded, &state); err != nil {
		_ = s.app.Redis.Del(ctx, recoveryStateKey(target, id)).Err()
		return recoveryState{}, nil
	}
	return state, nil
}
