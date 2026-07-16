package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/redis/go-redis/v9"

	authapi "github.com/yunloli/aiferry/api/auth"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
)

const (
	sessionCookieName = "aiferry_session"
	stateCookieName   = "aiferry_oauth_state"
	stateTTL          = 10 * time.Minute
	maxAuthBodySize   = 2 << 20
)

var (
	ErrInvalidState = errors.New("invalid oauth state")
	ErrAccessDenied = errors.New("account is not allowed to access AiFerry")
	ErrUnauthorized = errors.New("authentication required")
)

type contextKey string

const userContextKey contextKey = "aiferry.auth.user"

type Service struct {
	app *app.Service
}

type SessionUser struct {
	Id              uint64   `json:"id"`
	IdentitySubject string   `json:"identitySubject"`
	Name            string   `json:"name"`
	Role            string   `json:"role"`
	AvatarURL       string   `json:"avatarUrl"`
	Groups          []string `json:"groups"`
}

type oauthState struct {
	CallbackURL string `json:"callbackUrl"`
	ReturnTo    string `json:"returnTo"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
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
	Groups        []string `json:"groups"`
	IsAdmin       bool     `json:"isAdmin"`
	IsGlobalAdmin bool     `json:"isGlobalAdmin"`
	IsForbidden   bool     `json:"isForbidden"`
	IsDeleted     bool     `json:"isDeleted"`
	Disabled      bool     `json:"disabled"`
	Enabled       *bool    `json:"enabled"`
	Status        string   `json:"status"`
	DeletedTime   string   `json:"deletedTime"`
}

func New(appSvc *app.Service) *Service {
	return &Service{app: appSvc}
}

func (s *Service) Config() authapi.ConfigView {
	return authapi.ConfigView{Enabled: true, Provider: "Casdoor", LoginPath: "/api/auth/login"}
}

func (s *Service) BeginLogin(ctx context.Context, callbackURL, returnTo string) (string, string, error) {
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

func (s *Service) CompleteLogin(ctx context.Context, state, stateCookie, code, callbackURL string) (SessionUser, string, string, error) {
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
	if err = json.Unmarshal(stored, &metadata); err != nil || metadata.CallbackURL != callbackURL {
		return SessionUser{}, "", "", ErrInvalidState
	}
	accessToken, err := s.exchangeCode(ctx, code, callbackURL)
	if err != nil {
		return SessionUser{}, "", "", err
	}
	account, err := s.getAccount(ctx, accessToken)
	if err != nil {
		return SessionUser{}, "", "", err
	}
	if accountDisabled(account) || !accountAllowed(account, s.app.Config.CasdoorAllowedGroup) {
		return SessionUser{}, "", "", ErrAccessDenied
	}
	user, err := s.syncUser(ctx, account)
	if err != nil {
		return SessionUser{}, "", "", err
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
		return SessionUser{}, "", "", gerror.Wrap(err, "save login session")
	}
	return user, sessionToken, metadata.ReturnTo, nil
}

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
	_ = s.app.Redis.Expire(ctx, sessionKey(token), s.sessionTTL()).Err()
	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return gerror.Wrap(s.app.Redis.Del(ctx, sessionKey(token)).Err(), "delete login session")
}

func (s *Service) RequireAdmin(r *ghttp.Request) {
	user, err := s.Authenticate(r.Context(), r.Cookie.Get(sessionCookieName).String())
	if err != nil {
		writeUnauthorized(r)
		return
	}
	r.SetCtx(context.WithValue(r.Context(), userContextKey, user))
	r.Middleware.Next()
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
	host := strings.TrimSpace(r.GetHost())
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

func (s *Service) exchangeCode(ctx context.Context, code, callbackURL string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     s.app.Config.CasdoorClientID,
		"client_secret": s.app.Config.CasdoorClientSecret,
		"code":          code,
		"redirect_uri":  callbackURL,
	})
	if err != nil {
		return "", gerror.Wrap(err, "encode Casdoor token request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.app.Config.CasdoorEndpoint+"/api/login/oauth/access_token", bytes.NewReader(payload))
	if err != nil {
		return "", gerror.Wrap(err, "create Casdoor token request")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.app.HTTP.Do(req)
	if err != nil {
		return "", gerror.Wrap(err, "request Casdoor access token")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAuthBodySize))
	if err != nil {
		return "", gerror.Wrap(err, "read Casdoor token response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", gerror.Newf("Casdoor token exchange failed with HTTP %d", response.StatusCode)
	}
	var token tokenResponse
	if err = json.Unmarshal(body, &token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", gerror.New("Casdoor token response is invalid")
	}
	return token.AccessToken, nil
}

func (s *Service) getAccount(ctx context.Context, accessToken string) (casdoorAccount, error) {
	endpoint, err := url.Parse(s.app.Config.CasdoorEndpoint + "/api/get-account")
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "parse Casdoor account endpoint")
	}
	query := endpoint.Query()
	query.Set("access_token", accessToken)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "create Casdoor account request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := s.app.HTTP.Do(req)
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "request Casdoor account")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAuthBodySize))
	if err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "read Casdoor account response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return casdoorAccount{}, gerror.Newf("Casdoor account request failed with HTTP %d", response.StatusCode)
	}
	var envelope accountEnvelope
	if err = json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		if envelope.Status != "" && !strings.EqualFold(envelope.Status, "ok") {
			return casdoorAccount{}, gerror.New("Casdoor account request was rejected")
		}
		body = envelope.Data
	}
	var account casdoorAccount
	if err = json.Unmarshal(body, &account); err != nil {
		return casdoorAccount{}, gerror.Wrap(err, "decode Casdoor account")
	}
	if strings.TrimSpace(account.Uid) == "" && strings.TrimSpace(account.Id) == "" {
		return casdoorAccount{}, gerror.New("Casdoor account has no uid or id")
	}
	return account, nil
}

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
		Scan(&current); err != nil {
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

func sanitizeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\r\n") {
		return "/"
	}
	return value
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", gerror.Wrap(err, "generate secure token")
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func stateKey(state string) string {
	return "aiferry:oauth-state:" + state
}

func sessionKey(token string) string {
	return "aiferry:admin-session:" + token
}

func writeUnauthorized(r *ghttp.Request) {
	r.Response.WriteStatus(http.StatusUnauthorized)
	r.Response.WriteJson(map[string]any{"code": http.StatusUnauthorized, "message": "登录状态已失效", "data": nil})
	r.Exit()
}

func (u SessionUser) View() authapi.UserView {
	return authapi.UserView{Id: u.Id, Name: u.Name, Role: u.Role, AvatarURL: u.AvatarURL, Groups: u.Groups}
}

func (u SessionUser) String() string {
	return fmt.Sprintf("%s(%d)", u.Name, u.Id)
}
