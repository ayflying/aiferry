package auth

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/service/auth"
)

type Controller struct {
	auth *auth.Service
}

func New(authSvc *auth.Service) *Controller {
	return &Controller{auth: authSvc}
}

func (c *Controller) RegisterPublic(group *ghttp.RouterGroup) {
	group.GET("/config", c.config)
	group.GET("/login", c.login)
}

func (c *Controller) RegisterProtected(group *ghttp.RouterGroup) {
	group.Middleware(c.auth.RequireAdmin)
	group.GET("/me", c.me)
	group.POST("/logout", c.logout)
}

func (c *Controller) Callback(r *ghttp.Request) {
	callbackURL, err := auth.CallbackURL(r)
	if err != nil {
		redirectLoginError(r, "auth_failed")
		return
	}
	_, token, returnTo, err := c.auth.CompleteLogin(
		r.Context(),
		r.GetQuery("state").String(),
		r.Cookie.Get(auth.StateCookieName()).String(),
		r.GetQuery("code").String(),
		callbackURL,
	)
	clearCookie(r, auth.StateCookieName())
	if err != nil {
		g.Log().Errorf(r.Context(), "Casdoor callback failed: %v", err)
		switch {
		case errors.Is(err, auth.ErrInvalidState):
			redirectLoginError(r, "invalid_state")
		case errors.Is(err, auth.ErrAccessDenied):
			redirectLoginError(r, "access_denied")
		default:
			redirectLoginError(r, "auth_failed")
		}
		return
	}
	setCookie(r, auth.SessionCookieName(), token, c.auth.SessionTTL())
	r.Response.RedirectTo(returnTo, http.StatusFound)
}

func (c *Controller) config(r *ghttp.Request) {
	respond(r, c.auth.Config(), nil)
}

func (c *Controller) login(r *ghttp.Request) {
	callbackURL, err := auth.CallbackURL(r)
	if err != nil {
		respond(r, nil, err)
		return
	}
	loginURL, state, err := c.auth.BeginLogin(r.Context(), callbackURL, r.GetQuery("returnTo").String())
	if err != nil {
		respond(r, nil, err)
		return
	}
	setCookie(r, auth.StateCookieName(), state, 10*time.Minute)
	r.Response.RedirectTo(loginURL, http.StatusFound)
}

func (c *Controller) me(r *ghttp.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		respondUnauthorized(r)
		return
	}
	respond(r, user.View(), nil)
}

func (c *Controller) logout(r *ghttp.Request) {
	err := c.auth.Logout(r.Context(), r.Cookie.Get(auth.SessionCookieName()).String())
	clearCookie(r, auth.SessionCookieName())
	respond(r, map[string]any{}, err)
}

func setCookie(r *ghttp.Request, name, value string, ttl time.Duration) {
	r.Cookie.SetHttpCookie(&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   auth.SecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(r *ghttp.Request, name string) {
	r.Cookie.SetHttpCookie(&http.Cookie{
		Name:     name,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   auth.SecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func redirectLoginError(r *ghttp.Request, code string) {
	r.Response.RedirectTo("/login?error="+url.QueryEscape(code), http.StatusFound)
}

func respondUnauthorized(r *ghttp.Request) {
	r.Response.WriteStatus(http.StatusUnauthorized)
	r.Response.WriteJson(map[string]any{"code": http.StatusUnauthorized, "message": "登录状态已失效", "data": nil})
	r.Exit()
}

func respond(r *ghttp.Request, data any, err error) {
	if err != nil {
		r.Response.WriteStatus(http.StatusBadRequest)
		r.Response.WriteJson(map[string]any{"code": http.StatusBadRequest, "message": "认证请求失败", "data": nil})
		r.Exit()
		return
	}
	r.Response.WriteJson(map[string]any{"code": 0, "message": "", "data": data})
	r.Exit()
}
