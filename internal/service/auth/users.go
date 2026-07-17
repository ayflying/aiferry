package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *Service) syncUser(ctx context.Context, account casdoorAccount) (SessionUser, error) {
	var (
		uid    = accountUID(account)
		groups = append([]string(nil), account.Groups...)
		role   = "ai_user"
	)
	if account.IsAdmin || account.IsGlobalAdmin {
		role = "admin"
	}
	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return SessionUser{}, gerror.Wrap(err, "encode Casdoor groups")
	}
	columns := dao.Users.Columns()
	var current entity.Users
	if err = dao.Users.Ctx(ctx).
		Where(columns.IdentityProvider, "casdoor").
		Where(columns.IdentitySubject, uid).
		Scan(&current); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SessionUser{}, gerror.Wrap(err, "find Casdoor user")
	}
	if current.Id == 0 {
		if _, err = dao.Users.Ctx(ctx).Data(do.Users{
			Name:             accountName(account),
			Role:             role,
			Status:           1,
			IdentityProvider: "casdoor",
			IdentitySubject:  uid,
			AvatarUrl:        account.Avatar,
			GroupsJson:       string(groupsJSON),
			LastLoginAt:      time.Now(),
		}).InsertIgnore(); err != nil {
			return SessionUser{}, gerror.Wrap(err, "create Casdoor user")
		}
		if err = dao.Users.Ctx(ctx).
			Where(columns.IdentityProvider, "casdoor").
			Where(columns.IdentitySubject, uid).
			Scan(&current); err != nil {
			return SessionUser{}, gerror.Wrap(err, "load created Casdoor user")
		}
	}
	if current.Id == 0 || current.Status != 1 {
		return SessionUser{}, ErrAccessDenied
	}
	name := accountName(account)
	if _, err = dao.Users.Ctx(ctx).Where(columns.Id, current.Id).Data(do.Users{
		Name:        name,
		Role:        role,
		AvatarUrl:   account.Avatar,
		GroupsJson:  string(groupsJSON),
		LastLoginAt: time.Now(),
	}).Update(); err != nil {
		return SessionUser{}, gerror.Wrap(err, "refresh Casdoor user")
	}
	return SessionUser{
		Id:              current.Id,
		IdentitySubject: uid,
		Name:            name,
		Role:            role,
		AvatarURL:       account.Avatar,
		Groups:          groups,
	}, nil
}

func accountUID(account casdoorAccount) string {
	if uid := strings.TrimSpace(account.Uid); uid != "" {
		return uid
	}
	return strings.TrimSpace(account.Id)
}

func accountName(account casdoorAccount) string {
	if name := strings.TrimSpace(account.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(account.Name); name != "" {
		return name
	}
	return accountUID(account)
}

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

func accountAllowed(account casdoorAccount, allowedGroup string) bool {
	if account.IsAdmin || account.IsGlobalAdmin {
		return true
	}
	allowedGroup = strings.TrimSpace(allowedGroup)
	for _, group := range account.Groups {
		name := strings.TrimSpace(group)
		if index := strings.Index(name, "/"); index >= 0 {
			name = name[index+1:]
		}
		if name == allowedGroup {
			return true
		}
	}
	return false
}
