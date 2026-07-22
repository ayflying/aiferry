package apikey

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/auth"
	"github.com/yunloli/aiferry/internal/service/secret"
)

const cacheTTL = 5 * time.Minute

type Service struct {
	app *app.Service
}

type View struct {
	Id                   uint64     `json:"id"`
	UserId               uint64     `json:"userId"`
	Name                 string     `json:"name"`
	KeyPrefix            string     `json:"keyPrefix"`
	SecretAvailable      bool       `json:"secretAvailable"`
	Status               int        `json:"status"`
	SpendLimit           *float64   `json:"spendLimit"`
	DailySpendLimit      *float64   `json:"dailySpendLimit"`
	SpentAmount          float64    `json:"spentAmount"`
	AvailableAmount      *float64   `json:"availableAmount"`
	DailySpentAmount     float64    `json:"dailySpentAmount"`
	DailySpendDate       *time.Time `json:"dailySpendDate"`
	DailyAvailableAmount *float64   `json:"dailyAvailableAmount"`
	AllowedModels        []string   `json:"allowedModels"`
	ChannelGroupIDs      []uint64   `json:"channelGroupIds"`
	ExpiresAt            *time.Time `json:"expiresAt"`
	LastUsedAt           *time.Time `json:"lastUsedAt"`
	CreatedAt            time.Time  `json:"createdAt"`
}

type AuthKey struct {
	Id               uint64     `json:"id" orm:"id"`
	UserId           uint64     `json:"userId" orm:"user_id"`
	Name             string     `json:"name" orm:"name"`
	KeyHash          string     `json:"keyHash" orm:"key_hash"`
	Status           int        `json:"status" orm:"status"`
	SpendLimit       *float64   `json:"spendLimit" orm:"spend_limit"`
	DailySpendLimit  *float64   `json:"dailySpendLimit" orm:"daily_spend_limit"`
	SpentAmount      float64    `json:"spentAmount" orm:"spent_amount"`
	DailySpentAmount float64    `json:"dailySpentAmount" orm:"daily_spent_amount"`
	DailySpendDate   *time.Time `json:"dailySpendDate" orm:"daily_spend_date"`
	AllowedModels    []string   `json:"allowedModels"`
	ChannelGroupIDs  []uint64   `json:"channelGroupIds"`
	ExpiresAt        *time.Time `json:"expiresAt" orm:"expires_at"`
}

type Created struct {
	View
	Key string `json:"key"`
}

func New(appSvc *app.Service) *Service {
	return &Service{app: appSvc}
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	current, ok := auth.CurrentUser(ctx)
	if !ok {
		return nil, gerror.New("authenticated user is required")
	}
	rows := make([]View, 0)
	query := dao.ApiKeys.Ctx(ctx).
		Fields("id,user_id,name,key_prefix,key_cipher IS NOT NULL AND key_cipher <> '' AS secret_available,status,spend_limit,daily_spend_limit,spent_amount,daily_spent_amount,daily_spend_date,expires_at,last_used_at,created_at").
		OrderDesc(dao.ApiKeys.Columns().Id)
	if !s.app.Config.IsAdminRole(current.Role) {
		query = query.Where(dao.ApiKeys.Columns().UserId, current.Id)
	}
	err := query.Scan(&rows)
	if err != nil {
		return nil, gerror.Wrap(err, "list API keys")
	}
	for index := range rows {
		if err = s.populatePolicy(ctx, &rows[index]); err != nil {
			return nil, err
		}
	}
	return rows, nil
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
	keyCipher, err := s.app.Secrets.Encrypt(plainText)
	if err != nil {
		return Created{}, gerror.Wrap(err, "encrypt API key")
	}
	data := do.ApiKeys{
		UserId:    user.Id,
		Name:      strings.TrimSpace(input.Name),
		KeyPrefix: prefix,
		KeyHash:   hash,
		KeyCipher: keyCipher,
		Status:    1,
	}
	if input.SpendLimit != nil {
		data.SpendLimit = *input.SpendLimit
	}
	if input.DailySpendLimit != nil {
		data.DailySpendLimit = *input.DailySpendLimit
	}
	if input.ExpiresAt != nil {
		data.ExpiresAt = *input.ExpiresAt
	}
	var id uint64
	err = dao.ApiKeys.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		created, createErr := dao.ApiKeys.Ctx(txCtx).Data(data).InsertAndGetId()
		if createErr != nil {
			return gerror.Wrap(createErr, "create API key")
		}
		id = uint64(created)
		return s.replacePolicy(txCtx, id, input.AllowedModels, input.ChannelGroupIDs)
	})
	if err != nil {
		return Created{}, err
	}
	view := View{Id: id, UserId: user.Id, Name: input.Name, KeyPrefix: prefix, SecretAvailable: true, Status: 1, SpendLimit: input.SpendLimit, DailySpendLimit: input.DailySpendLimit, AllowedModels: normalizeModels(input.AllowedModels), ChannelGroupIDs: uniqueIDs(input.ChannelGroupIDs), ExpiresAt: input.ExpiresAt, CreatedAt: time.Now()}
	if input.SpendLimit != nil {
		available := *input.SpendLimit
		view.AvailableAmount = &available
	}
	if input.DailySpendLimit != nil {
		available := *input.DailySpendLimit
		view.DailyAvailableAmount = &available
	}
	return Created{
		View: view,
		Key:  plainText,
	}, nil
}

func (s *Service) Reveal(ctx context.Context, id uint64) (string, error) {
	var record entity.ApiKeys
	if err := dao.ApiKeys.Ctx(ctx).
		Fields(dao.ApiKeys.Columns().Id, dao.ApiKeys.Columns().UserId, dao.ApiKeys.Columns().KeyHash, dao.ApiKeys.Columns().KeyCipher).
		Where(dao.ApiKeys.Columns().Id, id).
		Scan(&record); err != nil {
		return "", gerror.Wrap(err, "find API key")
	}
	if record.Id == 0 {
		return "", gerror.New("API key not found")
	}
	if err := s.ensureAccess(ctx, record.UserId); err != nil {
		return "", err
	}
	if strings.TrimSpace(record.KeyCipher) == "" {
		return "", gerror.New("该访问密钥创建于完整密钥加密保存启用前，无法恢复，请创建新的访问密钥")
	}
	plainText, err := s.app.Secrets.Decrypt(record.KeyCipher)
	if err != nil {
		return "", gerror.Wrap(err, "decrypt API key")
	}
	if secret.HashAPIKey(plainText) != record.KeyHash {
		return "", gerror.New("访问密钥加密数据与摘要不匹配")
	}
	return plainText, nil
}

func (s *Service) Update(ctx context.Context, id uint64, input adminapi.APIKeyUpdate) error {
	var current AuthKey
	if err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().Id, id).Scan(&current); err != nil {
		return gerror.Wrap(err, "find API key")
	}
	if current.Id == 0 {
		return gerror.New("API key not found")
	}
	if err := s.ensureAccess(ctx, current.UserId); err != nil {
		return err
	}
	data := do.ApiKeys{Name: strings.TrimSpace(input.Name), Status: input.Status}
	if input.SpendLimit == nil {
		data.SpendLimit = gdb.Raw("NULL")
	} else {
		data.SpendLimit = *input.SpendLimit
	}
	if input.DailySpendLimit == nil {
		data.DailySpendLimit = gdb.Raw("NULL")
	} else {
		data.DailySpendLimit = *input.DailySpendLimit
	}
	if input.ExpiresAt == nil {
		data.ExpiresAt = gdb.Raw("NULL")
	} else {
		data.ExpiresAt = *input.ExpiresAt
	}
	if err := dao.ApiKeys.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, updateErr := dao.ApiKeys.Ctx(txCtx).Where(dao.ApiKeys.Columns().Id, id).Data(data).Update(); updateErr != nil {
			return gerror.Wrap(updateErr, "update API key")
		}
		return s.replacePolicy(txCtx, id, input.AllowedModels, input.ChannelGroupIDs)
	}); err != nil {
		return err
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
	if err := s.ensureAccess(ctx, current.UserId); err != nil {
		return err
	}
	if err := dao.ApiKeys.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, deleteErr := dao.ApiKeyModels.Ctx(txCtx).Where(dao.ApiKeyModels.Columns().ApiKeyId, id).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete key model policy")
		}
		if _, deleteErr := dao.ApiKeyChannelGroups.Ctx(txCtx).Where(dao.ApiKeyChannelGroups.Columns().ApiKeyId, id).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete key group policy")
		}
		if _, deleteErr := dao.ApiKeys.Ctx(txCtx).Where(dao.ApiKeys.Columns().Id, id).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete API key")
		}
		return nil
	}); err != nil {
		return err
	}
	return s.app.Redis.Del(ctx, cacheKey(current.KeyHash)).Err()
}

func (s *Service) ensureAccess(ctx context.Context, ownerID uint64) error {
	current, ok := auth.CurrentUser(ctx)
	if !ok {
		return gerror.New("authenticated user is required")
	}
	if current.Id != ownerID && !s.app.Config.IsAdminRole(current.Role) {
		return gerror.New("无权操作其他用户的访问密钥")
	}
	return nil
}
