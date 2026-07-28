// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"
	authapi "github.com/yunloli/aiferry/api/auth"
	. "github.com/yunloli/aiferry/internal/logic/auth"
)

type (
	IAuth interface {
		Config(ctx context.Context) (authapi.ConfigView, error)
		BeginLogin(ctx context.Context, callbackURL string, returnTo string) (string, string, error)
		CompleteLogin(ctx context.Context, state string, stateCookie string, code string) (SessionUser, string, string, error)
		View(user SessionUser) authapi.UserView
		Authenticate(ctx context.Context, token string) (SessionUser, error)
		Logout(ctx context.Context, token string) error
		RequireUser(r *ghttp.Request)
		RequireAdmin(r *ghttp.Request)
		RequireCurrentAdmin(r *ghttp.Request)
		IsAdmin(user SessionUser) bool
		SessionTTL() time.Duration
		// SetSessionCookie extends the browser session to match the Redis sliding expiry.
		SetSessionCookie(r *ghttp.Request, token string)
	}
)

var (
	localAuth IAuth
)

func Auth() IAuth {
	if localAuth == nil {
		panic("implement not found for interface IAuth, forgot register?")
	}
	return localAuth
}

func RegisterAuth(i IAuth) {
	localAuth = i
}
