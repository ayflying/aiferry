package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/redis/go-redis/v9"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *Service) Authenticate(ctx context.Context, token string) (SessionUser, error) {
	if token == "" {
		return SessionUser{}, ErrUnauthorized
	}
	encoded, err := s.app.Redis.Get(ctx, sessionKey(token)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return SessionUser{}, ErrUnauthorized
		}
		return SessionUser{}, gerror.Wrap(err, "read login session")
	}
	var user SessionUser
	if err = json.Unmarshal(encoded, &user); err != nil || user.Id == 0 {
		_ = s.app.Redis.Del(ctx, sessionKey(token)).Err()
		return SessionUser{}, ErrUnauthorized
	}
	var current entity.Users
	if err = dao.Users.Ctx(ctx).Where(dao.Users.Columns().Id, user.Id).Scan(&current); err != nil || current.Id == 0 || current.Status != 1 {
		_ = s.app.Redis.Del(ctx, sessionKey(token)).Err()
		return SessionUser{}, ErrUnauthorized
	}
	user.Name = current.Name
	user.Role = current.Role
	user.AvatarURL = current.AvatarUrl
	_ = s.app.Redis.Expire(ctx, sessionKey(token), s.sessionTTL()).Err()
	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return gerror.Wrap(s.app.Redis.Del(ctx, sessionKey(token)).Err(), "delete login session")
}

func (s *Service) RequireUser(r *ghttp.Request) {
	user, err := s.Authenticate(r.Context(), r.Cookie.Get(sessionCookieName).String())
	if err != nil {
		writeUnauthorized(r)
		return
	}
	r.SetCtx(context.WithValue(r.Context(), userContextKey, user))
	r.Middleware.Next()
}

func (s *Service) RequireAdmin(r *ghttp.Request) {
	user, err := s.Authenticate(r.Context(), r.Cookie.Get(sessionCookieName).String())
	if err != nil {
		writeUnauthorized(r)
		return
	}
	if !s.IsAdmin(user) {
		writeForbidden(r)
		return
	}
	r.SetCtx(context.WithValue(r.Context(), userContextKey, user))
	r.Middleware.Next()
}

func (s *Service) RequireCurrentAdmin(r *ghttp.Request) {
	user, ok := CurrentUser(r.Context())
	if !ok {
		writeUnauthorized(r)
		return
	}
	if !s.IsAdmin(user) {
		writeForbidden(r)
		return
	}
	r.Middleware.Next()
}

func (s *Service) IsAdmin(user SessionUser) bool {
	return s.app.Config.IsAdminRole(user.Role)
}

func CurrentUser(ctx context.Context) (SessionUser, bool) {
	user, ok := ctx.Value(userContextKey).(SessionUser)
	return user, ok && user.Id != 0
}

func SessionCookieName() string {
	return sessionCookieName
}

func StateCookieName() string {
	return stateCookieName
}

func (s *Service) SessionTTL() time.Duration {
	return s.sessionTTL()
}

func CallbackURL(r *ghttp.Request) (string, error) {
	scheme := strings.ToLower(strings.TrimSpace(strings.Split(r.GetHeader("X-Forwarded-Proto", r.GetSchema()), ",")[0]))
	if scheme != "http" && scheme != "https" {
		return "", gerror.New("invalid request scheme")
	}
	host := strings.TrimSpace(r.Host)
	if host == "" || strings.ContainsAny(host, "/\\\r\n") {
		return "", gerror.New("invalid request host")
	}
	return scheme + "://" + host + "/auth/casdoor/callback", nil
}

func SecureRequest(r *ghttp.Request) bool {
	proto := strings.ToLower(strings.TrimSpace(strings.Split(r.GetHeader("X-Forwarded-Proto", r.GetSchema()), ",")[0]))
	return proto == "https"
}

func (s *Service) sessionTTL() time.Duration {
	return time.Duration(s.app.Config.SessionTTL) * time.Hour
}

func writeUnauthorized(r *ghttp.Request) {
	r.Response.Status = http.StatusUnauthorized
	r.Response.WriteJson(map[string]any{"code": http.StatusUnauthorized, "message": "登录状态已失效", "data": nil})
	r.Exit()
}

func writeForbidden(r *ghttp.Request) {
	r.Response.Status = http.StatusForbidden
	r.Response.WriteJson(map[string]any{"code": http.StatusForbidden, "message": "需要管理员权限", "data": nil})
	r.Exit()
}
