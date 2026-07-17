package user

import (
	"context"
	"errors"
	"math"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/usage"
)

type Service struct {
	app   *app.Service
	usage *usage.Service
}

var ErrInsufficientBalance = gerror.New("账户余额不足，请先充值后再使用模型")

func IsInsufficientBalance(err error) bool {
	return errors.Is(err, ErrInsufficientBalance)
}

type Profile struct {
	Id          uint64     `json:"id"`
	Nickname    string     `json:"nickname"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	Balance     float64    `json:"balance"`
	AvatarURL   string     `json:"avatarUrl"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
}

type ManagedUser struct {
	Profile
	APIKeyCount int64             `json:"apiKeyCount"`
	Usage       usage.UserSummary `json:"usage"`
}

type apiKeyCache struct {
	Id      uint64 `orm:"id"`
	KeyHash string `orm:"key_hash"`
}

func New(appSvc *app.Service, usageSvc *usage.Service) *Service {
	return &Service{app: appSvc, usage: usageSvc}
}

func (s *Service) Profile(ctx context.Context, id uint64) (Profile, error) {
	user, err := s.find(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	return profileFromEntity(user), nil
}

func (s *Service) UpdateProfile(ctx context.Context, id uint64, nickname, email string) (Profile, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || utf8.RuneCountInString(nickname) > 64 {
		return Profile{}, gerror.New("昵称长度应为 1 到 64 个字符")
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return Profile{}, err
	}
	data := do.Users{Name: nickname}
	if email == "" {
		data.Email = gdb.Raw("NULL")
	} else {
		data.Email = email
	}
	if _, err = dao.Users.Ctx(ctx).Where(dao.Users.Columns().Id, id).Data(data).Update(); err != nil {
		return Profile{}, gerror.Wrap(err, "update user profile")
	}
	return s.Profile(ctx, id)
}

func (s *Service) Usage(ctx context.Context, id uint64, days int) (usage.UserSummary, error) {
	if _, err := s.find(ctx, id); err != nil {
		return usage.UserSummary{}, err
	}
	return s.usage.UserSummary(ctx, id, days)
}

func (s *Service) List(ctx context.Context) ([]ManagedUser, error) {
	rows := make([]entity.Users, 0)
	columns := dao.Users.Columns()
	if err := dao.Users.Ctx(ctx).
		Where(columns.IdentityProvider, "casdoor").
		OrderDesc(columns.Id).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list Casdoor users")
	}
	result := make([]ManagedUser, 0, len(rows))
	for _, row := range rows {
		summary, err := s.usage.UserSummary(ctx, row.Id, 30)
		if err != nil {
			return nil, err
		}
		keyCount, err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().UserId, row.Id).Count()
		if err != nil {
			return nil, gerror.Wrap(err, "count user API keys")
		}
		result = append(result, ManagedUser{Profile: profileFromEntity(row), APIKeyCount: int64(keyCount), Usage: summary})
	}
	return result, nil
}

func (s *Service) UpdateBalance(ctx context.Context, id uint64, balance float64) (Profile, error) {
	if math.IsNaN(balance) || math.IsInf(balance, 0) || balance < 0 {
		return Profile{}, gerror.New("余额必须是非负金额")
	}
	if _, err := s.find(ctx, id); err != nil {
		return Profile{}, err
	}
	if _, err := dao.Users.Ctx(ctx).Where(dao.Users.Columns().Id, id).Data(do.Users{Balance: balance}).Update(); err != nil {
		return Profile{}, gerror.Wrap(err, "update user balance")
	}
	return s.Profile(ctx, id)
}

func (s *Service) CheckBalance(ctx context.Context, id uint64) error {
	count, err := dao.Users.Ctx(ctx).
		Where(dao.Users.Columns().Id, id).
		WhereGT(dao.Users.Columns().Balance, 0).
		Count()
	if err != nil {
		return gerror.Wrap(err, "check user balance")
	}
	if count == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

func (s *Service) Debit(ctx context.Context, id uint64, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	amount = amount.Round(8)
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	literal := amount.StringFixed(8)
	result, err := dao.Users.Ctx(ctx).
		Where(dao.Users.Columns().Id, id).
		WhereGTE(dao.Users.Columns().Balance, literal).
		Data(do.Users{Balance: gdb.Raw("balance - " + literal)}).
		Update()
	if err != nil {
		return gerror.Wrap(err, "debit user balance")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrInsufficientBalance
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, id, operatorID uint64) error {
	if id == usage.SystemUserID {
		return gerror.New("系统用户不能删除")
	}
	if id == operatorID {
		return gerror.New("不能删除当前登录用户")
	}
	target, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if target.IdentityProvider != "casdoor" {
		return gerror.New("只能删除 Casdoor 同步用户")
	}
	keys := make([]apiKeyCache, 0)
	if err = dao.ApiKeys.Ctx(ctx).Unscoped().
		Fields(dao.ApiKeys.Columns().Id, dao.ApiKeys.Columns().KeyHash).
		Where(dao.ApiKeys.Columns().UserId, id).
		Scan(&keys); err != nil {
		return gerror.Wrap(err, "list user API keys for deletion")
	}
	keyIDs := make([]uint64, 0, len(keys))
	cacheKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		keyIDs = append(keyIDs, key.Id)
		cacheKeys = append(cacheKeys, "aiferry:api-key:"+key.KeyHash)
	}
	if err = dao.Users.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, deleteErr := dao.UsageLogs.Ctx(txCtx).Where(dao.UsageLogs.Columns().UserId, id).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete user usage logs")
		}
		if len(keyIDs) > 0 {
			if _, deleteErr := dao.ApiKeyModels.Ctx(txCtx).WhereIn(dao.ApiKeyModels.Columns().ApiKeyId, keyIDs).Delete(); deleteErr != nil {
				return gerror.Wrap(deleteErr, "delete user API key model policies")
			}
			if _, deleteErr := dao.ApiKeyChannelGroups.Ctx(txCtx).WhereIn(dao.ApiKeyChannelGroups.Columns().ApiKeyId, keyIDs).Delete(); deleteErr != nil {
				return gerror.Wrap(deleteErr, "delete user API key channel policies")
			}
		}
		if _, deleteErr := dao.ApiKeys.Ctx(txCtx).Unscoped().Where(dao.ApiKeys.Columns().UserId, id).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete user API keys")
		}
		if _, deleteErr := dao.Users.Ctx(txCtx).Unscoped().Where(dao.Users.Columns().Id, id).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete user")
		}
		return nil
	}); err != nil {
		return err
	}
	if len(cacheKeys) > 0 {
		_ = s.app.Redis.Del(ctx, cacheKeys...).Err()
	}
	return nil
}

func (s *Service) find(ctx context.Context, id uint64) (entity.Users, error) {
	var result entity.Users
	if err := dao.Users.Ctx(ctx).Where(dao.Users.Columns().Id, id).Scan(&result); err != nil {
		return result, gerror.Wrap(err, "find user")
	}
	if result.Id == 0 {
		return result, gerror.New("用户不存在")
	}
	return result, nil
}

func profileFromEntity(value entity.Users) Profile {
	profile := Profile{
		Id:        value.Id,
		Nickname:  value.Name,
		Email:     value.Email,
		Role:      value.Role,
		Balance:   value.Balance,
		AvatarURL: value.AvatarUrl,
		CreatedAt: value.CreatedAt,
	}
	if !value.LastLoginAt.IsZero() {
		profile.LastLoginAt = &value.LastLoginAt
	}
	return profile
}

func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 320 {
		return "", gerror.New("邮箱长度不能超过 320 个字符")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return "", gerror.New("邮箱格式无效")
	}
	return strings.ToLower(value), nil
}
