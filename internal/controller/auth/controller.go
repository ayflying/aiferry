package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	authapi "github.com/yunloli/aiferry/api/auth"
	"github.com/yunloli/aiferry/internal/service/auth"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/user"
)

type Controller struct {
	auth     *auth.Service
	users    *user.Service
	settings *system.Service
}

func New(authSvc *auth.Service, userSvc *user.Service, systemSvc *system.Service) *Controller {
	return &Controller{auth: authSvc, users: userSvc, settings: systemSvc}
}

func (c *Controller) RegisterPublic(group *ghttp.RouterGroup) {
	group.GET("/config", c.config)
	group.GET("/login", c.login)
}

func (c *Controller) RegisterProtected(group *ghttp.RouterGroup) {
	group.Middleware(c.auth.RequireUser)
	group.GET("/me", c.me)
	group.GET("/profile", c.profile)
	group.PUT("/profile", c.updateProfile)
	group.GET("/usage", c.personalUsage)
	group.POST("/logout", c.logout)
}

func (c *Controller) Callback(r *ghttp.Request) {
	_, token, returnTo, err := c.auth.CompleteLogin(
		r.Context(),
		r.GetQuery("state").String(),
		r.Cookie.Get(auth.StateCookieName()).String(),
		r.GetQuery("code").String(),
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
	c.auth.SetSessionCookie(r, token)
	r.Response.RedirectTo(returnTo, http.StatusFound)
}

func (c *Controller) config(r *ghttp.Request) {
	data, err := c.auth.Config(r.Context())
	if err != nil {
		respond(r, nil, err)
		return
	}
	data.System = c.publicSystemInformation(r)
	respond(r, data, nil)
}

func (c *Controller) login(r *ghttp.Request) {
	callbackURL, err := c.callbackURL(r)
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

func (c *Controller) callbackURL(r *ghttp.Request) (string, error) {
	information, err := c.settings.GetSystemInformation(r.Context())
	if err == nil && information.ServerURL != "" {
		return information.ServerURL + "/auth/casdoor/callback", nil
	}
	return auth.CallbackURL(r)
}

func (c *Controller) publicSystemInformation(r *ghttp.Request) authapi.SystemInformationView {
	information := system.DefaultSystemInformation()
	fallback, err := requestServerURL(r)
	if err == nil {
		if loaded, loadErr := c.settings.ResolveSystemInformation(r.Context(), fallback); loadErr == nil {
			information = loaded
		} else {
			g.Log().Warningf(r.Context(), "load public system information: %v", loadErr)
			information.ServerURL = fallback
		}
	}
	return authapi.SystemInformationView{
		SystemName: information.SystemName, ServerURL: information.ServerURL, LogoURL: information.LogoURL,
		Footer: information.Footer, About: information.About, HomeContent: information.HomeContent,
		UserAgreement: information.UserAgreement, PrivacyPolicy: information.PrivacyPolicy,
	}
}

func requestServerURL(r *ghttp.Request) (string, error) {
	callbackURL, err := auth.CallbackURL(r)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(callbackURL, "/auth/casdoor/callback"), nil
}

func (c *Controller) me(r *ghttp.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		respondUnauthorized(r)
		return
	}
	respond(r, c.auth.View(user), nil)
}

func (c *Controller) profile(r *ghttp.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		respondUnauthorized(r)
		return
	}
	data, err := c.users.Profile(r.Context(), user.Id)
	respond(r, data, err)
}

func (c *Controller) updateProfile(r *ghttp.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		respondUnauthorized(r)
		return
	}
	var input authapi.ProfileUpdateInput
	if err := r.Parse(&input); err != nil {
		respond(r, nil, err)
		return
	}
	data, err := c.users.UpdateProfile(r.Context(), user.Id, input.Nickname, input.Email)
	respond(r, data, err)
}

func (c *Controller) personalUsage(r *ghttp.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		respondUnauthorized(r)
		return
	}
	data, err := c.users.Usage(r.Context(), user.Id, r.GetQuery("days", 30).Int())
	respond(r, data, err)
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
	r.Response.Status = http.StatusUnauthorized
	r.Response.WriteJson(map[string]any{"code": http.StatusUnauthorized, "message": "登录状态已失效", "data": nil})
	r.Exit()
}

func respond(r *ghttp.Request, data any, err error) {
	if err != nil {
		r.Response.Status = http.StatusBadRequest
		r.Response.WriteJson(map[string]any{"code": http.StatusBadRequest, "message": "认证请求失败", "data": nil})
		r.Exit()
		return
	}
	r.Response.WriteJson(map[string]any{"code": 0, "message": "", "data": data})
	r.Exit()
}
