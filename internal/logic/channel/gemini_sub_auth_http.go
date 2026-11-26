package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	GeminiOAuthCallbackPath   = "/api/oauth/gemini-subscription/callback"
	geminiOAuthAuthorizeURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	geminiOAuthTokenURL       = "https://oauth2.googleapis.com/token"
	geminiOAuthUserInfoURL    = "https://openidconnect.googleapis.com/v1/userinfo"
	geminiOAuthScope          = "openid https://www.googleapis.com/auth/cloud-platform"
	geminiOAuthRequestTimeout = 12 * time.Second
	geminiOAuthBodyLimit      = 64 << 10
)

type geminiOAuthConfig struct {
	clientID     string
	clientSecret string
	redirectURI  string
}

func readGeminiOAuthConfig() (geminiOAuthConfig, error) {
	cfg := geminiOAuthConfig{
		clientID:     strings.TrimSpace(os.Getenv("AIFERRY_GEMINI_OAUTH_CLIENT_ID")),
		clientSecret: strings.TrimSpace(os.Getenv("AIFERRY_GEMINI_OAUTH_CLIENT_SECRET")),
		redirectURI:  strings.TrimSpace(os.Getenv("AIFERRY_GEMINI_OAUTH_REDIRECT_URI")),
	}
	if cfg.clientID == "" || cfg.clientSecret == "" {
		return cfg, gerror.New("Gemini OAuth 未配置，请联系运营方配置自有 Google OAuth 客户端")
	}
	return cfg, nil
}

func (cfg geminiOAuthConfig) validateRedirectURI() error {
	parsed, err := url.Parse(cfg.redirectURI)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.EscapedPath() != GeminiOAuthCallbackPath || parsed.ForceQuery || parsed.RawQuery != "" || strings.Contains(cfg.redirectURI, "#") {
		return gerror.New("Gemini OAuth 回调必须配置为本站 HTTPS 地址，路径为 " + GeminiOAuthCallbackPath + "，且不能带参数或片段")
	}
	return nil
}

func (cfg geminiOAuthConfig) authorizationURL(state, challenge string, picker bool) string {
	scope := geminiOAuthScope
	if picker {
		scope += " " + geminiProjectReadOnlyScope
	}
	return geminiOAuthAuthorizeURL + "?" + url.Values{
		"client_id": {cfg.clientID}, "redirect_uri": {cfg.redirectURI},
		"response_type": {"code"}, "scope": {scope}, "access_type": {"offline"},
		"prompt": {"consent"}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}.Encode()
}

type geminiOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func exchangeGeminiOAuthTokens(ctx context.Context, client *http.Client, cfg geminiOAuthConfig, form url.Values) (geminiOAuthTokens, error) {
	var tokens geminiOAuthTokens
	form.Set("client_id", cfg.clientID)
	form.Set("client_secret", cfg.clientSecret)
	if err := geminiOAuthRequest(ctx, client, geminiOAuthTokenURL, form, "", &tokens); err != nil {
		return tokens, err
	}
	if !strings.EqualFold(tokens.TokenType, "Bearer") || !validGeminiOAuthToken(tokens.AccessToken) || tokens.ExpiresIn <= 0 || tokens.ExpiresIn > 86400 || (tokens.RefreshToken != "" && !validGeminiOAuthToken(tokens.RefreshToken)) {
		return tokens, gerror.New("Google 返回了无效的 Gemini OAuth 令牌")
	}
	if tokens.Scope != "" {
		scopes := strings.Fields(tokens.Scope)
		for _, required := range strings.Fields(geminiOAuthScope) {
			granted := false
			for _, scope := range scopes {
				granted = granted || scope == required
			}
			if !granted {
				return tokens, gerror.New("Google 未授予 Gemini 所需权限，请重新登录并同意所需权限")
			}
		}
	}
	return tokens, nil
}

func validGeminiOAuthToken(token string) bool {
	if token == "" || len(token) > 8192 {
		return false
	}
	for _, char := range token {
		if char <= 32 || char >= 127 {
			return false
		}
	}
	return true
}

func geminiOAuthSubject(ctx context.Context, client *http.Client, accessToken string) (string, error) {
	var reply struct {
		Subject string `json:"sub"`
	}
	if err := geminiOAuthRequest(ctx, client, geminiOAuthUserInfoURL, nil, accessToken, &reply); err != nil {
		return "", err
	}
	if !validGeminiOAuthToken(reply.Subject) || len(reply.Subject) > 255 {
		return "", gerror.New("Google OAuth 未返回有效的账号标识")
	}
	return reply.Subject, nil
}

// 仅供固定 Google API 的内部调用；不输出响应内容或可能包含代理凭据的错误。
func geminiOAuthRequest(ctx context.Context, client *http.Client, endpoint string, form url.Values, bearer string, dest any) error {
	ctx, cancel := context.WithTimeout(ctx, geminiOAuthRequestTimeout)
	defer cancel()
	method, body := http.MethodGet, strings.NewReader("")
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return gerror.New("无法创建 Google OAuth 请求")
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	requestClient := *client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := requestClient.Do(req)
	if err != nil {
		return gerror.New("通过渠道代理访问 Google OAuth 失败，请检查代理配置")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return gerror.Newf("Google OAuth 拒绝授权（HTTP %d），请检查 OAuth 客户端配置并重新登录", resp.StatusCode)
		}
		return gerror.Newf("Google OAuth 请求失败（HTTP %d）", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, geminiOAuthBodyLimit+1))
	if err != nil || len(payload) > geminiOAuthBodyLimit || json.Unmarshal(payload, dest) != nil {
		return gerror.New("Google OAuth 响应无效")
	}
	return nil
}
