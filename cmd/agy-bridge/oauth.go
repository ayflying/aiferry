package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 官方 Antigravity 桌面客户端的 OAuth 常量。客户端凭据本身从环境变量读取（见下），
// 这里只放端点与固定值。
//
// 为什么桥接要自己跑 OAuth：官方 CLI 从 1.1.15 起在未登录时不再打印授权地址，
// 只报 "You are not logged into Antigravity"，因此「在容器里交互式执行 CLI 完成
// 登录」这条路不可用；只能按官方客户端的方式跑一遍标准 OAuth 2.0 + PKCE。
const (
	antigravityAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityTokenURL     = "https://oauth2.googleapis.com/token"
	antigravityUserInfoURL  = "https://www.googleapis.com/oauth2/v1/userinfo"
	// 官方客户端注册的固定回环回调。授权在用户自己的浏览器里完成，回调打到
	// 用户本机而不是桥接容器，所以桥接收不到它——由用户把回调地址粘回管理端。
	antigravityRedirectURI = "http://localhost:51121/oauth-callback"
	// 令牌在账号 HOME 中的位置，与官方 CLI 自己写入的路径保持一致。
	antigravityTokenPath = ".gemini/antigravity-cli/antigravity-oauth-token"
	// 一次授权会话的有效期。Google 授权码本身约 10 分钟内有效。
	loginSessionTTL = 10 * time.Minute
)

// 官方客户端内置的 OAuth 客户端凭据通过环境变量注入，不写进源码：它们会被 GitHub
// 的 push protection 判定为泄漏密钥，而且官方轮换凭据时应当只改部署配置。
// 未配置时只影响「发起新登录」，已有登录态的对话与额度查询照常可用。
const (
	oauthClientIDEnv     = "AGY_BRIDGE_OAUTH_CLIENT_ID"
	oauthClientSecretEnv = "AGY_BRIDGE_OAUTH_CLIENT_SECRET"
)

// antigravityOAuthClient 读取官方客户端的公开客户端凭据。
func antigravityOAuthClient() (string, string, error) {
	clientID := strings.TrimSpace(os.Getenv(oauthClientIDEnv))
	clientSecret := strings.TrimSpace(os.Getenv(oauthClientSecretEnv))
	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf("account login requires %s and %s", oauthClientIDEnv, oauthClientSecretEnv)
	}
	return clientID, clientSecret, nil
}

// antigravityScopes 是官方客户端申请的权限集。openid 绝对不能加：带上它会让
// Google 把这个客户端路由到 firstparty/nativeapp 那套会卡住的同意流程。
var antigravityScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// antigravityToken 是官方 CLI 令牌文件里的 token 段。
type antigravityToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
}

// antigravityTokenDoc 是官方 CLI 的令牌文件结构：
// {"token": {...}, "auth_method": "consumer"}。
type antigravityTokenDoc struct {
	Token      antigravityToken `json:"token"`
	AuthMethod string           `json:"auth_method"`
}

// loginSession 是一次进行中的授权：PKCE 校验串只在内存里，绑定发起它的账号。
type loginSession struct {
	account  string
	verifier string
	expires  time.Time
}

// loginSessions 保存在内存里的授权会话。桥接重启会丢失未完成的会话，
// 这是可接受的：用户重新点一次授权即可，不会产生半登录状态。
type loginSessions struct {
	mu    sync.Mutex
	items map[string]loginSession
}

func newLoginSessions() *loginSessions {
	return &loginSessions{items: make(map[string]loginSession)}
}

// start 生成新的 PKCE 串与授权地址，state 作为该会话的键。
func (s *loginSessions) start(accountName string) (string, string, error) {
	clientID, _, err := antigravityOAuthClient()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	state, err := randomToken(18)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	query := url.Values{}
	query.Set("client_id", clientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", antigravityRedirectURI)
	query.Set("scope", strings.Join(antigravityScopes, " "))
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("access_type", "offline")
	// 强制每次都返回 refresh_token：重复授权同一账号时也要能续期。
	query.Set("prompt", "consent")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.items[state] = loginSession{account: accountName, verifier: verifier, expires: time.Now().Add(loginSessionTTL)}
	return state, antigravityAuthorizeURL + "?" + query.Encode(), nil
}

// take 取走一个会话的 PKCE 校验串：会话一次性使用，无论成功失败都不再保留，
// 并且只允许发起它的那个桥接账号来兑现。
func (s *loginSessions) take(state, accountName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.items[state]
	if !ok {
		return "", false
	}
	delete(s.items, state)
	if session.account != accountName || time.Now().After(session.expires) {
		return "", false
	}
	return session.verifier, true
}

// pruneLocked 清掉已过期的会话，避免长期运行后内存里堆着无用的授权串。
func (s *loginSessions) pruneLocked() {
	now := time.Now()
	for key, session := range s.items {
		if now.After(session.expires) {
			delete(s.items, key)
		}
	}
}

// randomToken 生成 URL 安全的随机串，用于 PKCE verifier 与 state。
func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// parseLoginInput 解析用户粘回来的内容：既接受完整的回环回调地址
// （http://localhost:51121/oauth-callback?code=...），也接受裸授权码。
func parseLoginInput(input string) (string, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return "", errors.New("authorization input is empty")
	}
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		parsed, err := url.Parse(text)
		if err != nil {
			return "", errors.New("authorization URL is not valid")
		}
		if message := parsed.Query().Get("error"); message != "" {
			return "", fmt.Errorf("authorization was rejected: %s", message)
		}
		code := parsed.Query().Get("code")
		if code == "" {
			return "", errors.New("authorization URL does not contain a code")
		}
		return code, nil
	}
	// 裸授权码：Google 的形态形如 "4/1Af..."，这里只拒绝明显非法的空白与换行。
	if strings.ContainsAny(text, " \r\n\t") {
		return "", errors.New("authorization code contains whitespace")
	}
	return text, nil
}

// oauthHTTPClient 构造访问 Google 认证端点用的客户端，代理规则与官方 CLI 一致：
// 请求头指定 > 账号固定配置 > 编排全局代理。回环地址永远不走代理。
func oauthHTTPClient(proxy string) (*http.Client, error) {
	fromProxy, err := proxyFunc(proxy)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: fromProxy},
		Timeout:   30 * time.Second,
	}, nil
}

// exchangeAntigravityCode 用授权码换取令牌。返回的错误只描述失败类别，
// 不回显响应正文，避免把含令牌的报文写进日志。
func exchangeAntigravityCode(ctx context.Context, code, verifier, proxy string) (antigravityTokenDoc, error) {
	clientID, clientSecret, err := antigravityOAuthClient()
	if err != nil {
		return antigravityTokenDoc{}, err
	}
	client, err := oauthHTTPClient(proxy)
	if err != nil {
		return antigravityTokenDoc{}, err
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", antigravityRedirectURI)
	form.Set("code_verifier", verifier)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return antigravityTokenDoc{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := client.Do(request)
	if err != nil {
		return antigravityTokenDoc{}, fmt.Errorf("token endpoint is unreachable: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return antigravityTokenDoc{}, err
	}
	if response.StatusCode != http.StatusOK {
		return antigravityTokenDoc{}, fmt.Errorf("token endpoint rejected the code: HTTP %d", response.StatusCode)
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return antigravityTokenDoc{}, errors.New("token endpoint returned unreadable JSON")
	}
	if payload.AccessToken == "" {
		return antigravityTokenDoc{}, errors.New("token endpoint returned no access token")
	}
	// 没有 refresh_token 说明这次授权无法续期，登录态很快会失效；
	// 直接拒绝，让用户重新授权，而不是写进一份会静默过期的凭据。
	if payload.RefreshToken == "" {
		return antigravityTokenDoc{}, errors.New("token endpoint returned no refresh token")
	}

	tokenType := payload.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return antigravityTokenDoc{
		Token: antigravityToken{
			AccessToken:  payload.AccessToken,
			TokenType:    tokenType,
			RefreshToken: payload.RefreshToken,
			// 官方 CLI 用 Go 的 RFC3339 解析该字段，带毫秒同样合法。
			Expiry: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).Format("2006-01-02T15:04:05.000Z07:00"),
		},
		AuthMethod: "consumer",
	}, nil
}

// antigravityAccountEmail 读取该令牌对应的邮箱，便于管理员确认登录的是哪个账号。
// 取不到不算失败：邮箱只用于展示，不影响登录态本身。
func antigravityAccountEmail(ctx context.Context, accessToken, proxy string) string {
	client, err := oauthHTTPClient(proxy)
	if err != nil {
		return ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, antigravityUserInfoURL, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return ""
	}
	var profile struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(body, &profile) != nil {
		return ""
	}
	return strings.TrimSpace(profile.Email)
}

// writeAntigravityToken 把令牌按官方格式写进账号 HOME。先写临时文件再改名，
// 保证官方 CLI 任何时刻读到的都是完整文件，不会读到写了一半的 JSON。
func writeAntigravityToken(home string, doc antigravityTokenDoc) error {
	directory := filepath.Join(home, filepath.Dir(antigravityTokenPath))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	target := filepath.Join(home, antigravityTokenPath)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, payload, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}
