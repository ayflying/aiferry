package apikey

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/auth"
	"github.com/yunloli/aiferry/internal/service/secret"
)

const cacheTTL = 5 * time.Minute

type Service struct {
	app *app.Service
}

type View struct {
	Id         uint64     `json:"id"`
	UserId     uint64     `json:"userId"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"`
	Status     int        `json:"status"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type AuthKey struct {
	Id        uint64     `json:"id" orm:"id"`
	UserId    uint64     `json:"userId" orm:"user_id"`
	Name      string     `json:"name" orm:"name"`
	KeyHash   string     `json:"keyHash" orm:"key_hash"`
	Status    int        `json:"status" orm:"status"`
	ExpiresAt *time.Time `json:"expiresAt" orm:"expires_at"`
}

type Created struct {
	View
	Key string `json:"key"`
}

func New(appSvc *app.Service) *Service {
	return &Service{app: appSvc}
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	rows := make([]View, 0)
	err := dao.ApiKeys.Ctx(ctx).
		Fields("id,user_id,name,key_prefix,status,expires_at,last_used_at,created_at").
		OrderDesc(dao.ApiKeys.Columns().Id).
		Scan(&rows)
	return rows, gerror.Wrap(err, "list API keys")
}

func (s *Service) Create(ctx context.Context, input adminapi.APIKeyInput) (Created, error) {
	user, ok := auth.CurrentUser(ctx)
	if !ok {
		return Created{}, gerror.New("authenticated user is required")
	}
	plainText, prefix, hash, err := secret.GenerateAPIKey()
	if err != nil {
		return Created{}, err
	}
	data := do.ApiKeys{
		UserId:    user.Id,
		Name:      strings.TrimSpace(input.Name),
		KeyPrefix: prefix,
		KeyHash:   hash,
		Status:    1,
	}
	if input.ExpiresAt != nil {
		data.ExpiresAt = *input.ExpiresAt
	}
	id, err := dao.ApiKeys.Ctx(ctx).Data(data).InsertAndGetId()
	if err != nil {
		return Created{}, gerror.Wrap(err, "create API key")
	}
	return Created{
		View: View{
			Id:        uint64(id),
			UserId:    user.Id,
			Name:      input.Name,
			KeyPrefix: prefix,
			Status:    1,
			ExpiresAt: input.ExpiresAt,
			CreatedAt: time.Now(),
		},
		Key: plainText,
	}, nil
}

func (s *Service) Update(ctx context.Context, id uint64, input adminapi.APIKeyUpdate) error {
	var current AuthKey
	if err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().Id, id).Scan(&current); err != nil {
		return gerror.Wrap(err, "find API key")
	}
	if current.Id == 0 {
		return gerror.New("API key not found")
	}
	data := do.ApiKeys{Name: strings.TrimSpace(input.Name), Status: input.Status}
	if input.ExpiresAt == nil {
		data.ExpiresAt = gdb.Raw("NULL")
	} else {
		data.ExpiresAt = *input.ExpiresAt
	}
	if _, err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().Id, id).Data(data).Update(); err != nil {
		return gerror.Wrap(err, "update API key")
	}
	return s.app.Redis.Del(ctx, cacheKey(current.KeyHash)).Err()
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	var current AuthKey
	if err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().Id, id).Scan(&current); err != nil {
		return gerror.Wrap(err, "find API key")
	}
	if current.Id == 0 {
		return gerror.New("API key not found")
	}
	if _, err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().Id, id).Delete(); err != nil {
		return gerror.Wrap(err, "delete API key")
	}
	return s.app.Redis.Del(ctx, cacheKey(current.KeyHash)).Err()
}

func (s *Service) Authenticate(ctx context.Context, bearer string) (AuthKey, error) {
	if !strings.HasPrefix(bearer, "af_") {
		return AuthKey{}, gerror.New("invalid API key")
	}
	hash := secret.HashAPIKey(bearer)
	key, err := s.getCached(ctx, hash)
	if err != nil {
		return AuthKey{}, err
	}
	if key.Id == 0 || key.Status != 1 || (key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now())) {
		return AuthKey{}, gerror.New("invalid or expired API key")
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
		Fields("id,user_id,name,key_hash,status,expires_at").
		Where(dao.ApiKeys.Columns().KeyHash, hash).
		Scan(&key); err != nil {
		return AuthKey{}, gerror.Wrap(err, "find API key")
	}
	if key.Id != 0 {
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

func cacheKey(hash string) string {
	return "aiferry:api-key:" + hash
}
