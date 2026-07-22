package apikey

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/service/secret"
)

func (s *Service) Authenticate(ctx context.Context, bearer string) (AuthKey, error) {
	if !secret.HasAPIKeyPrefix(bearer) {
		return AuthKey{}, gerror.New("invalid API key")
	}
	hash := secret.HashAPIKey(bearer)
	key, err := s.getCached(ctx, hash)
	if err != nil {
		return AuthKey{}, err
	}
	if key.Id == 0 || key.Status != 1 || (key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now())) || (key.SpendLimit != nil && key.SpentAmount >= *key.SpendLimit) {
		return AuthKey{}, gerror.New("invalid or expired API key")
	}
	if key.dailyLimitReached(time.Now()) {
		return AuthKey{}, ErrDailySpendLimitReached
	}
	s.touchLastUsed(key.Id)
	return key, nil
}

func (s *Service) getCached(ctx context.Context, hash string) (AuthKey, error) {
	var key AuthKey
	value, err := s.app.Redis.Get(ctx, cacheKey(hash)).Result()
	if err == nil {
		if json.Unmarshal([]byte(value), &key) == nil {
			return key, nil
		}
	} else if err != redis.Nil {
		return AuthKey{}, gerror.Wrap(err, "read API key cache")
	}
	if err = dao.ApiKeys.Ctx(ctx).
		Fields("id,user_id,name,key_hash,status,spend_limit,daily_spend_limit,spent_amount,daily_spent_amount,daily_spend_date,expires_at").
		Where(dao.ApiKeys.Columns().KeyHash, hash).
		Scan(&key); err != nil {
		return AuthKey{}, gerror.Wrap(err, "find API key")
	}
	if key.Id != 0 {
		if err = s.populateAuthPolicy(ctx, &key); err != nil {
			return AuthKey{}, err
		}
		encoded, _ := json.Marshal(key)
		_ = s.app.Redis.Set(ctx, cacheKey(hash), encoded, cacheTTL).Err()
	}
	return key, nil
}

func (s *Service) touchLastUsed(id uint64) {
	ctx := context.Background()
	lockKey := fmt.Sprintf("aiferry:api-key-touch:%d", id)
	if ok, _ := s.app.Redis.SetNX(ctx, lockKey, "1", time.Minute).Result(); !ok {
		return
	}
	_, _ = dao.ApiKeys.Ctx(ctx).
		Where(dao.ApiKeys.Columns().Id, id).
		Data(do.ApiKeys{LastUsedAt: time.Now()}).
		Update()
}

func (s *Service) CanUseModel(key AuthKey, model string) bool {
	return len(key.AllowedModels) == 0 || contains(key.AllowedModels, model)
}

func (s *Service) CanUseChannelGroups(key AuthKey, groupIDs []uint64) bool {
	if len(key.ChannelGroupIDs) == 0 {
		return true
	}
	for _, groupID := range groupIDs {
		if containsID(key.ChannelGroupIDs, groupID) {
			return true
		}
	}
	return false
}
