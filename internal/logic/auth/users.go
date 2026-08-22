package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// syncUser 将 Casdoor 返回的外部身份同步到本地 users 表。
//
// 外部身份和本地账户分开保存：Casdoor 负责证明“用户是谁以及账号是否可用”，
// AiFerry 负责保存余额、访问密钥、用量等业务数据。IdentityProvider 与
// IdentitySubject 组成稳定的外部身份键，不能用邮箱替代，因为邮箱可能为空或变更。
func (s *sAuth) syncUser(ctx context.Context, account casdoorAccount) (SessionUser, error) {
	uid := accountUID(account)
	role := accountRole(account)
	columns := dao.Users.Columns()

	// 先按外部身份查找，避免用户更换显示名称或邮箱后产生重复本地账户。
	var current entity.Users
	if err := dao.Users.Ctx(ctx).
		Where(columns.IdentityProvider, "casdoor").
		Where(columns.IdentitySubject, uid).
		Scan(&current); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SessionUser{}, gerror.Wrap(err, "find Casdoor user")
	}

	if current.Id == 0 {
		// 首次登录时创建本地镜像。Status=1 只表示本地账户刚创建且可用，
		// 之后每次登录仍会重新检查 Casdoor 的禁用状态。
		if _, err := dao.Users.Ctx(ctx).Data(do.Users{
			Name:             accountName(account),
			Email:            strings.TrimSpace(account.Email),
			Role:             role,
			Status:           1,
			IdentityProvider: "casdoor",
			IdentitySubject:  uid,
			AvatarUrl:        account.Avatar,
			LastLoginAt:      time.Now(),
		}).InsertIgnore(); err != nil {
			return SessionUser{}, gerror.Wrap(err, "create Casdoor user")
		}

		// InsertIgnore 可能因为并发登录而没有插入新行，因此必须重新查询，
		// 不能直接假设当前请求持有刚插入的自增 ID。
		if err := dao.Users.Ctx(ctx).
			Where(columns.IdentityProvider, "casdoor").
			Where(columns.IdentitySubject, uid).
			Scan(&current); err != nil {
			return SessionUser{}, gerror.Wrap(err, "load created Casdoor user")
		}
	}

	// 普通 Casdoor 用户和 Casdoor 管理员都允许登录；角色只影响本地业务权限。
	// Casdoor 账号状态已在 CompleteLogin 中检查，这里只检查本地账户是否存在且启用。
	if current.Id == 0 || current.Status != 1 {
		return SessionUser{}, ErrAccessDenied
	}

	// 只同步会变化的展示字段和登录时间，不覆盖余额、密钥等本地业务数据。
	if _, err := dao.Users.Ctx(ctx).Where(columns.Id, current.Id).Data(do.Users{
		Role:        role,
		AvatarUrl:   account.Avatar,
		LastLoginAt: time.Now(),
	}).Update(); err != nil {
		return SessionUser{}, gerror.Wrap(err, "refresh Casdoor user")
	}

	return SessionUser{
		Id:              current.Id,
		IdentitySubject: uid,
		Name:            accountName(account),
		Role:            role,
		AvatarURL:       account.Avatar,
	}, nil
}

// accountUID 选择 Casdoor 的稳定身份标识。uid 是首选，旧版本或部分部署只返回 id。
func accountUID(account casdoorAccount) string {
	if uid := strings.TrimSpace(account.Uid); uid != "" {
		return uid
	}
	return strings.TrimSpace(account.Id)
}

// accountName 按展示名、登录名、外部身份 ID 的顺序生成本地显示名称。
func accountName(account casdoorAccount) string {
	if name := strings.TrimSpace(account.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(account.Name); name != "" {
		return name
	}
	return accountUID(account)
}

// accountRole 只做 Casdoor 管理员到本地角色的映射，不参与“能否登录”的判断。
// 非管理员返回 user，登录资格由 CompleteLogin 和 accountDisabled 决定。
func accountRole(account casdoorAccount) string {
	if account.IsAdmin || account.IsGlobalAdmin {
		return "admin"
	}
	return "user"
}

// accountDisabled 统一解释 Casdoor 不同版本可能返回的账号状态字段。
// Enabled 使用指针是为了区分“字段缺失”和“明确返回 false”；字段缺失不应误拒绝用户。
func accountDisabled(account casdoorAccount) bool {
	if account.IsForbidden || account.IsDeleted || account.Disabled || strings.TrimSpace(account.DeletedTime) != "" {
		return true
	}
	if account.Enabled != nil && !*account.Enabled {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(account.Status))
	return status == "disabled" || status == "deleted" || status == "inactive" || status == "forbidden"
}
