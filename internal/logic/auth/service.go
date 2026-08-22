package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"

	authapi "github.com/yunloli/aiferry/api/auth"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/system"
)

const (
	// sessionCookieName 保存登录成功后签发的随机会话令牌；令牌本身不包含用户资料。
	sessionCookieName = "aiferry_session"
	// stateCookieName 与 Redis 中的 OAuth state 双重校验，防止回调被伪造或串用。
	stateCookieName = "aiferry_oauth_state"
	// stateTTL 限制 OAuth 登录流程的有效窗口，过期后必须重新发起登录。
	stateTTL = 10 * time.Minute
	// maxAuthBodySize 限制 Casdoor 响应体，避免异常上游响应占用过多内存。
	maxAuthBodySize = 2 << 20
)

var (
	ErrInvalidState = errors.New("invalid oauth state")
	ErrAccessDenied = errors.New("account is not allowed to access AiFerry")
	ErrUnauthorized = errors.New("authentication required")
)

type contextKey string

const userContextKey contextKey = "aiferry.auth.user"

type sAuth struct {
	app      *app.Service
	settings *system.Service
}

type SessionUser struct {
	Id              uint64   `json:"id"`
	IdentitySubject string   `json:"identitySubject"`
	Name            string   `json:"name"`
	Role            string   `json:"role"`
	AvatarURL       string   `json:"avatarUrl"`
}

type oauthState struct {
	CallbackURL string `json:"callbackUrl"`
	ReturnTo    string `json:"returnTo"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

type tokenErrorResponse struct {
	Error string `json:"error"`
}

type accountEnvelope struct {
	Status string          `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

type casdoorAccount struct {
	Uid           string   `json:"uid"`
	Id            string   `json:"id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName"`
	Avatar        string   `json:"avatar"`
	Email         string   `json:"email"`
	IsAdmin       bool     `json:"isAdmin"`
	IsGlobalAdmin bool     `json:"isGlobalAdmin"`
	IsForbidden   bool     `json:"isForbidden"`
	IsDeleted     bool     `json:"isDeleted"`
	Disabled      bool     `json:"disabled"`
	Enabled       *bool    `json:"enabled"`
	Status        string   `json:"status"`
	DeletedTime   string   `json:"deletedTime"`
}

func New(appSvc *app.Service, settingsSvc *system.Service) *sAuth {
	return &sAuth{app: appSvc, settings: settingsSvc}
}

func (s *sAuth) Config(ctx context.Context) (authapi.ConfigView, error) {
	settings, err := s.settings.GetBase(ctx)
	if err != nil {
		return authapi.ConfigView{}, err
	}
	return authapi.ConfigView{Enabled: true, Provider: "Casdoor", LoginPath: "/api/auth/login", TimeZone: settings.TimeZone}, nil
}

// BeginLogin 创建一次性 OAuth 登录状态，并生成 Casdoor 授权地址。
// callbackURL 会写入 Redis，回调时使用同一地址交换 code，避免代理环境下地址被篡改。
func (s *sAuth) BeginLogin(ctx context.Context, callbackURL, returnTo string) (string, string, error) {
	state, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	stored, err := json.Marshal(oauthState{CallbackURL: callbackURL, ReturnTo: sanitizeReturnTo(returnTo)})
	if err != nil {
		return "", "", gerror.Wrap(err, "encode OAuth state")
	}
	if err = s.app.Redis.Set(ctx, stateKey(state), stored, stateTTL).Err(); err != nil {
		return "", "", gerror.Wrap(err, "save OAuth state")
	}
	values := url.Values{
		"client_id":     {s.app.Config.CasdoorClientID},
		"response_type": {"code"},
		"redirect_uri":  {callbackURL},
		"scope":         {"read:users openid profile email"},
		"state":         {state},
	}
	return s.app.Config.CasdoorEndpoint + "/login/oauth/authorize?" + values.Encode(), state, nil
}

// CompleteLogin 完成 OAuth code 交换、Casdoor 账户校验、本地用户同步和会话签发。
// 任何一步失败都不会创建本地登录会话；普通用户与管理员的区别只在 role，
// 不在这里做用户组准入判断。
func (s *sAuth) CompleteLogin(ctx context.Context, state, stateCookie, code string) (SessionUser, string, string, error) {
	// 同时校验 URL state、浏览器 Cookie 和 Redis state，确保回调属于当前登录流程。
	if state == "" || code == "" || stateCookie == "" || state != stateCookie {
		return SessionUser{}, "", "", ErrInvalidState
	}
	stored, err := s.app.Redis.GetDel(ctx, stateKey(state)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return SessionUser{}, "", "", ErrInvalidState
		}
		return SessionUser{}, "", "", gerror.Wrap(err, "read OAuth state")
	}
	var metadata oauthState
	if err = json.Unmarshal(stored, &metadata); err != nil || metadata.CallbackURL == "" {
		return SessionUser{}, "", "", ErrInvalidState
	}
	accessToken, err := s.exchangeCode(ctx, code, metadata.CallbackURL)
	if err != nil {
		return SessionUser{}, "", "", gerror.Wrap(err, "exchange Casdoor authorization code")
	}
	account, err := s.getAccount(ctx, accessToken)
	if err != nil {
		return SessionUser{}, "", "", gerror.Wrap(err, "fetch Casdoor account")
	}
	// Casdoor 返回的禁用、删除或禁止状态优先于本地缓存；这样管理员在 Casdoor
	// 侧停用账号后，旧的 AiFerry 会话也无法继续创建或刷新。
	if accountDisabled(account) {
		return SessionUser{}, "", "", ErrAccessDenied
	}
	user, err := s.syncUser(ctx, account)
	if err != nil {
		return SessionUser{}, "", "", gerror.Wrap(err, "synchronize Casdoor account")
	}
	sessionToken, err := randomToken(32)
	if err != nil {
		return SessionUser{}, "", "", err
	}
	encoded, err := json.Marshal(user)
	if err != nil {
		return SessionUser{}, "", "", gerror.Wrap(err, "encode login session")
	}
	if err = s.app.Redis.Set(ctx, sessionKey(sessionToken), encoded, s.sessionTTL()).Err(); err != nil {
		return SessionUser{}, "", "", gerror.Wrap(err, "save Casdoor login session")
	}
	return user, sessionToken, metadata.ReturnTo, nil
}

func (s *sAuth) View(user SessionUser) authapi.UserView {
	return authapi.UserView{
		Id: user.Id, Name: user.Name, Role: user.Role, IsAdmin: s.IsAdmin(user), AvatarURL: user.AvatarURL,
	}
}

func (u SessionUser) String() string {
	return fmt.Sprintf("%s(%d)", u.Name, u.Id)
}
