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
		return SessionUser{}, "", "", gerror.Wrap(err, "exchange Casdoor authorization code")
	}
	account, err := s.getAccount(ctx, accessToken)
	if err != nil {
		return SessionUser{}, "", "", gerror.Wrap(err, "fetch Casdoor account")
	}
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

func (s *Service) View(user SessionUser) authapi.UserView {
	return authapi.UserView{
		Id: user.Id, Name: user.Name, Role: user.Role, IsAdmin: s.IsAdmin(user), AvatarURL: user.AvatarURL,
	}
}

func (u SessionUser) String() string {
	return fmt.Sprintf("%s(%d)", u.Name, u.Id)
}
