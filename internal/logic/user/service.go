package user

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type sUser struct {
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
	IsAdmin         bool              `json:"isAdmin"`
	IsAdminOverride bool              `json:"isAdminOverride"`
	APIKeyCount     int64             `json:"apiKeyCount"`
	ChannelGroups   []string          `json:"channelGroups"`
	Usage           usage.UserSummary `json:"usage"`
}

type Option struct {
	Id       uint64 `json:"id" orm:"id"`
	Nickname string `json:"nickname" orm:"name"`
}

type apiKeyCache struct {
	Id      uint64 `orm:"id"`
	KeyHash string `orm:"key_hash"`
}

type userAdminOverrideDO struct {
	g.Meta       `orm:"table:user_admin_overrides, do:true"`
	UserID       any `orm:"user_id"`
	OriginalRole any `orm:"original_role"`
}

type userAdminOverride struct {
	UserID       uint64 `orm:"user_id"`
	OriginalRole string `orm:"original_role"`
}

func New(appSvc *app.Service, usageSvc *usage.Service) *sUser {
	return &sUser{app: appSvc, usage: usageSvc}
}

func (s *sUser) Profile(ctx context.Context, id uint64) (Profile, error) {
	user, err := s.find(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	return profileFromEntity(user), nil
}

func (s *sUser) UpdateProfile(ctx context.Context, id uint64, nickname, email string) (Profile, error) {
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

func (s *sUser) Usage(ctx context.Context, id uint64, days int) (usage.UserSummary, error) {
	if _, err := s.find(ctx, id); err != nil {
		return usage.UserSummary{}, err
	}
	return s.usage.UserSummary(ctx, id, days)
}

func (s *sUser) List(ctx context.Context) ([]ManagedUser, error) {
	rows := make([]entity.Users, 0)
	columns := dao.Users.Columns()
	if err := dao.Users.Ctx(ctx).
		Where(columns.IdentityProvider, "casdoor").
		OrderDesc(columns.Id).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list Casdoor users")
	}
	userIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.Id)
	}
	groupRows := make([]struct {
		UserID uint64 `orm:"user_id"`
		Name   string `orm:"name"`
	}, 0)
	if len(rows) > 0 {
		if err := g.DB().Model("user_channel_groups ucg").
			Ctx(ctx).
			LeftJoin("channel_groups cg", "cg.id = ucg.channel_group_id").
			Fields("ucg.user_id", "cg.name").
			WhereIn("ucg.user_id", userIDs).
			Order("cg.name").
			Scan(&groupRows); err != nil {
			return nil, gerror.Wrap(err, "list user channel groups")
		}
	}
	groupsByUser := make(map[uint64][]string, len(rows))
	for _, group := range groupRows {
		groupsByUser[group.UserID] = append(groupsByUser[group.UserID], group.Name)
	}
	overrideRows := make([]userAdminOverride, 0)
	if len(userIDs) > 0 {
		if err := g.DB().Model("user_admin_overrides").Ctx(ctx).Fields("user_id").WhereIn("user_id", userIDs).Scan(&overrideRows); err != nil {
			return nil, gerror.Wrap(err, "list local administrator overrides")
		}
	}
	adminOverrides := make(map[uint64]struct{}, len(overrideRows))
	for _, row := range overrideRows {
		adminOverrides[row.UserID] = struct{}{}
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
		profile := profileFromEntity(row)
		isAdmin := s.app.Config.IsAdminRole(row.Role)
		if _, overridden := adminOverrides[row.Id]; overridden {
			isAdmin = true
			if len(s.app.Config.AdminRoles) > 0 {
				profile.Role = s.app.Config.AdminRoles[0]
			} else {
				profile.Role = "admin"
			}
		}
		_, isAdminOverride := adminOverrides[row.Id]
		result = append(result, ManagedUser{Profile: profile, IsAdmin: isAdmin, IsAdminOverride: isAdminOverride, APIKeyCount: int64(keyCount), ChannelGroups: groupsByUser[row.Id], Usage: summary})
	}
	return result, nil
}

func (s *sUser) ListOptions(ctx context.Context) ([]Option, error) {
	rows := make([]Option, 0)
	columns := dao.Users.Columns()
	if err := dao.Users.Ctx(ctx).
		Fields(columns.Id, columns.Name).
		Where(columns.IdentityProvider, "casdoor").
		OrderDesc(columns.Id).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list user options")
	}
	return rows, nil
}

func (s *sUser) AdminEmails(ctx context.Context) ([]string, error) {
	rows := make([]struct {
		Email string `orm:"email"`
	}, 0)
	columns := dao.Users.Columns()
	if err := dao.Users.Ctx(ctx).
		Fields(columns.Email).
		WhereIn(columns.Role, s.app.Config.AdminRoles).
		Where(columns.Status, 1).
		WhereNotNull(columns.Email).
		OrderAsc(columns.Id).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list administrator emails")
	}
	emails := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		email := strings.ToLower(strings.TrimSpace(row.Email))
		if email == "" {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}
	return emails, nil
}

func (s *sUser) UpdateBalance(ctx context.Context, id uint64, balance float64) (Profile, error) {
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

// 余额存在性缓存：计费模型的每个转发请求都会执行一次 CheckBalance 的
// COUNT 查询，高并发下压力显著。这里用 2 秒 TTL 的 Redis 标记缓存"用户
// 余额>0"的判定结果；余额扣减与充值写路径会立即失效缓存。真正的防负
// 余额由 Debit 的 SELECT ... FOR UPDATE 事务兜底，缓存窗口内最坏情况是
// 用户余额刚耗尽后仍可再发 2 秒内的少量请求（随后在扣费事务中被拒），
// 属可接受的准确性权衡。
const balanceCacheTTL = 2 * time.Second

func balanceCacheKey(id uint64) string {
	return fmt.Sprintf("aiferry:balance-ok:%d", id)
}

func (s *sUser) CheckBalance(ctx context.Context, id uint64) error {
	exists, err := s.app.Redis.Exists(ctx, balanceCacheKey(id)).Result()
	if err == nil && exists > 0 {
		return nil
	}
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
	_ = s.app.Redis.Set(ctx, balanceCacheKey(id), 1, balanceCacheTTL).Err()
	return nil
}

func (s *sUser) invalidateBalanceCache(ctx context.Context, id uint64) {
	_ = s.app.Redis.Del(ctx, balanceCacheKey(id)).Err()
}

func (s *sUser) Debit(ctx context.Context, id uint64, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	amount = amount.Round(8)
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	return dao.Users.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		columns := dao.Users.Columns()
		var account entity.Users
		if err := dao.Users.Ctx(txCtx).
			Fields(columns.Id, columns.Balance).
			Where(columns.Id, id).
			Lock(gdb.LockForUpdate).
			Scan(&account); err != nil {
			return gerror.Wrap(err, "lock user balance for debit")
		}
		balance := decimal.NewFromFloat(account.Balance).Round(8)
		if account.Id == 0 || balance.LessThan(amount) {
			return ErrInsufficientBalance
		}
		updatedBalance, _ := balance.Sub(amount).Round(8).Float64()
		if _, err := dao.Users.Ctx(txCtx).
			Where(columns.Id, account.Id).
			Data(do.Users{Balance: updatedBalance}).
			Update(); err != nil {
			return gerror.Wrap(err, "debit user balance")
		}
		return nil
	})
}

func (s *sUser) Credit(ctx context.Context, id uint64, amount decimal.Decimal) error {
	amount = amount.Round(8)
	if amount.LessThanOrEqual(decimal.Zero) {
		return gerror.New("充值金额必须大于零")
	}
	return dao.Users.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		columns := dao.Users.Columns()
		var account entity.Users
		if err := dao.Users.Ctx(txCtx).
			Fields(columns.Id, columns.Balance).
			Where(columns.Id, id).
			Lock(gdb.LockForUpdate).
			Scan(&account); err != nil {
			return gerror.Wrap(err, "lock user balance for credit")
		}
		if account.Id == 0 {
			return gerror.New("用户不存在")
		}
		updatedBalance, _ := decimal.NewFromFloat(account.Balance).Add(amount).Round(8).Float64()
		if _, err := dao.Users.Ctx(txCtx).
			Where(columns.Id, account.Id).
			Data(do.Users{Balance: updatedBalance}).
			Update(); err != nil {
			return gerror.Wrap(err, "credit user balance")
		}
		s.invalidateBalanceCache(txCtx, account.Id)
		return nil
	})
}

func (s *sUser) SetAdmin(ctx context.Context, id, operatorID uint64, enabled bool) error {
	if id == 0 {
		return gerror.New("用户不存在")
	}
	if id == operatorID && !enabled {
		return gerror.New("不能移除自己的管理员权限")
	}
	return dao.Users.Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		if _, err := tx.Model("user_admin_role_lock").Ctx(txCtx).Where("id", 1).Lock(gdb.LockForUpdate).One(); err != nil {
			return gerror.Wrap(err, "lock administrator role changes")
		}
		columns := dao.Users.Columns()
		var lockedUsers []entity.Users
		if err := tx.Model("users").Ctx(txCtx).
			Fields(columns.Id, columns.Role, columns.Status, columns.IdentityProvider).
			Where(columns.IdentityProvider, "casdoor").
			Where(columns.Status, 1).
			Lock(gdb.LockForUpdate).
			Scan(&lockedUsers); err != nil {
			return gerror.Wrap(err, "lock user accounts for role update")
		}
		adminIDs := make(map[uint64]struct{}, len(lockedUsers))
		for _, account := range lockedUsers {
			if s.app.Config.IsAdminRole(account.Role) {
				adminIDs[account.Id] = struct{}{}
			}
		}
		var target entity.Users
		if err := dao.Users.Ctx(txCtx).Where(columns.Id, id).Lock(gdb.LockForUpdate).Scan(&target); err != nil {
			return gerror.Wrap(err, "find user for role update")
		}
		if target.Id == 0 || target.IdentityProvider != "casdoor" {
			return gerror.New("只能修改 Casdoor 用户的管理员角色")
		}
		var override userAdminOverride
		if err := tx.Model("user_admin_overrides").Ctx(txCtx).Where("user_id", id).Scan(&override); err != nil {
			return gerror.Wrap(err, "load original user role")
		}
		if enabled {
			if override.OriginalRole != "" {
				return nil
			}
			if s.app.Config.IsAdminRole(target.Role) {
				if _, err := tx.Model("user_admin_overrides").Ctx(txCtx).Data(userAdminOverrideDO{UserID: id, OriginalRole: target.Role}).Insert(); err != nil {
					return gerror.Wrap(err, "save original user role")
				}
				return nil
			}
			if _, err := tx.Model("user_admin_overrides").Ctx(txCtx).Data(userAdminOverrideDO{UserID: id, OriginalRole: target.Role}).Insert(); err != nil {
				return gerror.Wrap(err, "save original user role")
			}
			role := "admin"
			if len(s.app.Config.AdminRoles) > 0 {
				role = s.app.Config.AdminRoles[0]
			}
			if _, err := dao.Users.Ctx(txCtx).Where(columns.Id, id).Data(do.Users{Role: role}).Update(); err != nil {
				return gerror.Wrap(err, "grant administrator role")
			}
			return nil
		}
		if override.OriginalRole == "" {
			return gerror.New("该管理员由 Casdoor 授权，请在 Casdoor 管理")
		}
		if _, stillAdmin := adminIDs[id]; !stillAdmin {
			return gerror.New("该用户当前不是管理员")
		}
		if _, stillAdmin := adminIDs[id]; !stillAdmin {
			return gerror.New("该用户当前不是管理员")
		}
		if len(adminIDs) <= 1 {
			return gerror.New("不能移除最后一位管理员")
		}
		if _, err := tx.Model("user_admin_overrides").Ctx(txCtx).Where("user_id", id).Delete(); err != nil {
			return gerror.Wrap(err, "remove local administrator override")
		}
		if _, err := dao.Users.Ctx(txCtx).Where(columns.Id, id).Data(do.Users{Role: override.OriginalRole}).Update(); err != nil {
			return gerror.Wrap(err, "restore original user role")
		}
		return nil
	})
}

func (s *sUser) Delete(ctx context.Context, id, operatorID uint64) error {
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

func (s *sUser) ListChannelGroupIDs(ctx context.Context, id uint64) ([]uint64, error) {
	if _, err := s.find(ctx, id); err != nil {
		return nil, err
	}
	rows := make([]struct {
		ChannelGroupID uint64 `orm:"channel_group_id"`
	}, 0)
	if err := g.DB().Model("user_channel_groups").
		Ctx(ctx).
		Fields("channel_group_id").
		Where("user_id", id).
		Order("channel_group_id").
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list user channel groups")
	}
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ChannelGroupID)
	}
	return ids, nil
}

func (s *sUser) ReplaceChannelGroupIDs(ctx context.Context, id uint64, groupIDs []uint64) ([]uint64, error) {
	if _, err := s.find(ctx, id); err != nil {
		return nil, err
	}
	uniqueGroupIDs := make([]uint64, 0, len(groupIDs))
	seen := make(map[uint64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID == 0 {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		uniqueGroupIDs = append(uniqueGroupIDs, groupID)
	}
	if err := dao.ChannelGroups.Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		if _, err := tx.Model("user_channel_groups").Ctx(txCtx).Where("user_id", id).Delete(); err != nil {
			return gerror.Wrap(err, "clear user channel groups")
		}
		for _, groupID := range uniqueGroupIDs {
			if _, err := tx.Model("user_channel_groups").Ctx(txCtx).Data(g.Map{"user_id": id, "channel_group_id": groupID}).Insert(); err != nil {
				return gerror.Wrap(err, "insert user channel group")
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	persistedGroupIDs, err := s.ListChannelGroupIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(persistedGroupIDs) != len(uniqueGroupIDs) {
		return persistedGroupIDs, gerror.Newf("用户渠道分组保存校验失败：请求 %d 个，实际保存 %d 个", len(uniqueGroupIDs), len(persistedGroupIDs))
	}
	persisted := make(map[uint64]struct{}, len(persistedGroupIDs))
	for _, groupID := range persistedGroupIDs {
		persisted[groupID] = struct{}{}
	}
	for _, groupID := range uniqueGroupIDs {
		if _, exists := persisted[groupID]; !exists {
			return persistedGroupIDs, gerror.New("用户渠道分组保存校验失败：实际关联与请求不一致")
		}
	}
	return persistedGroupIDs, nil
}

func (s *sUser) find(ctx context.Context, id uint64) (entity.Users, error) {
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
