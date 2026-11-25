package channel

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const geminiLoginTTL = 10 * time.Minute

type geminiLoginSession struct {
	ChannelID   uint64
	UserID      uint64
	ExpiresAt   time.Time
	RedirectURI string
	Verifier    string
	Credential  *geminiSubscriptionCredential
	Result      *ChannelLoginResult
	Failed      bool
}

type geminiLoginCallback struct {
	Code   string
	Denied bool
}

func geminiLoginKey(state string) string {
	return "aiferry:gemini-login:" + upstreamKeyHash(state)
}

func validGeminiLoginState(state string) bool {
	if len(state) != 64 || strings.ToLower(state) != state {
		return false
	}
	_, err := hex.DecodeString(state)
	return err == nil
}

func (s *sChannel) startGeminiLogin(ctx context.Context, channel entity.Channels) (ChannelLoginSession, error) {
	user, ok := auth.CurrentUser(ctx)
	if !ok {
		return ChannelLoginSession{}, gerror.New("未登录")
	}
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		return ChannelLoginSession{}, err
	}
	if err = cfg.validateRedirectURI(); err != nil {
		return ChannelLoginSession{}, err
	}
	if s.app.Redis == nil {
		return ChannelLoginSession{}, gerror.New("Gemini 登录需要可用的会话缓存")
	}
	if _, err = s.GeminiSubscriptionHTTPClient(ctx, channel, 0); err != nil {
		return ChannelLoginSession{}, err
	}
	state, err := randomCodexState()
	if err != nil {
		return ChannelLoginSession{}, err
	}
	verifier, challenge, err := geminiPKCE()
	if err != nil {
		return ChannelLoginSession{}, err
	}
	session := geminiLoginSession{ChannelID: channel.Id, UserID: user.Id, ExpiresAt: time.Now().Add(geminiLoginTTL), RedirectURI: cfg.redirectURI, Verifier: verifier}
	if err = s.saveGeminiLogin(ctx, cfg, state, session); err != nil {
		return ChannelLoginSession{}, err
	}
	return ChannelLoginSession{State: state, AuthURL: cfg.authorizationURL(state, challenge), IntervalSeconds: 3, ExpiresInSeconds: int(geminiLoginTTL.Seconds())}, nil
}

func (s *sChannel) saveGeminiLogin(ctx context.Context, cfg geminiOAuthConfig, state string, session geminiLoginSession) error {
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return gerror.New("Gemini 登录会话已过期")
	}
	sealed, err := cfg.seal("login:"+state, session)
	if err != nil {
		return err
	}
	if err = s.app.Redis.Set(ctx, geminiLoginKey(state), sealed, ttl).Err(); err != nil {
		return gerror.New("保存 Gemini 登录会话失败")
	}
	return nil
}

func (s *sChannel) readGeminiLogin(ctx context.Context, cfg geminiOAuthConfig, state string) (geminiLoginSession, error) {
	var session geminiLoginSession
	if s.app.Redis == nil || !validGeminiLoginState(state) {
		return session, gerror.New("Gemini 登录会话无效或已过期")
	}
	sealed, err := s.app.Redis.Get(ctx, geminiLoginKey(state)).Result()
	if err != nil || cfg.open("login:"+state, sealed, &session) != nil || !time.Now().Before(session.ExpiresAt) || session.RedirectURI != cfg.redirectURI {
		return session, gerror.New("Gemini 登录会话无效或已过期")
	}
	return session, nil
}

// AcceptGeminiOAuthCallback 仅记录一次 Google 回调，不在公开路由换票或保存渠道凭据。
// 凭据写入必须由发起者通过已认证渠道的 PollChannelLogin 完成。
func (s *sChannel) AcceptGeminiOAuthCallback(ctx context.Context, state, code, oauthError string) error {
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		return err
	}
	session, err := s.readGeminiLogin(ctx, cfg, state)
	if err != nil {
		return err
	}
	if (code == "") == (oauthError == "") || (code != "" && !validGeminiOAuthToken(code)) {
		return gerror.New("Gemini OAuth 回调参数无效")
	}
	if session.Result != nil || session.Failed || session.Credential != nil {
		return gerror.New("Gemini OAuth 回调已处理")
	}
	sealed, err := cfg.seal("callback:"+state, geminiLoginCallback{Code: code, Denied: oauthError != ""})
	if err != nil {
		return err
	}
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return gerror.New("Gemini 登录会话已过期")
	}
	accepted, err := s.app.Redis.SetNX(ctx, geminiLoginKey(state)+":callback", sealed, ttl).Result()
	if err != nil || !accepted {
		return gerror.New("Gemini OAuth 回调已处理或暂时不可用")
	}
	return nil
}

func (s *sChannel) pollGeminiLogin(ctx context.Context, channel entity.Channels, state string) (ChannelLoginResult, error) {
	user, ok := auth.CurrentUser(ctx)
	if !ok {
		return ChannelLoginResult{}, gerror.New("未登录")
	}
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		return ChannelLoginResult{}, err
	}
	session, err := s.readGeminiLogin(ctx, cfg, state)
	if err != nil || session.ChannelID != channel.Id || session.UserID != user.Id {
		return ChannelLoginResult{}, gerror.New("Gemini 登录会话无效或已过期")
	}
	if session.Result != nil {
		return *session.Result, nil
	}
	if session.Failed {
		return ChannelLoginResult{}, gerror.New("Gemini 授权未完成，请重新发起登录")
	}
	pending := ChannelLoginResult{Status: "pending"}
	key := geminiLoginKey(state)
	sealed, err := s.app.Redis.Get(ctx, key+":callback").Result()
	if errors.Is(err, cache.ErrMiss) && session.Credential == nil {
		return pending, nil
	}
	if err != nil && !errors.Is(err, cache.ErrMiss) {
		return ChannelLoginResult{}, gerror.New("读取 Gemini OAuth 回调失败")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	acquired, err := s.app.Redis.SetNX(ctx, key+":poll", "1", 45*time.Second).Result()
	if err != nil {
		return ChannelLoginResult{}, gerror.New("锁定 Gemini 登录会话失败")
	}
	if !acquired {
		return pending, nil
	}
	// 获锁后重新读取，确保回调只换票一次，临时数据库失败只重试持久化。
	session, err = s.readGeminiLogin(ctx, cfg, state)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	if session.Result != nil {
		return *session.Result, nil
	}
	if session.Failed {
		return ChannelLoginResult{}, gerror.New("Gemini 授权未完成，请重新发起登录")
	}
	if session.Credential == nil {
		var cb geminiLoginCallback
		if err = cfg.open("callback:"+state, sealed, &cb); err != nil {
			return ChannelLoginResult{}, err
		}
		if err = s.exchangeGeminiLogin(ctx, cfg, channel, key, &session, cb); err != nil {
			if session.Failed {
				persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				_ = s.saveGeminiLogin(persistCtx, cfg, state, session)
				persistCancel()
			}
			return ChannelLoginResult{}, err
		}
		if err = s.saveGeminiLogin(ctx, cfg, state, session); err != nil {
			return ChannelLoginResult{}, gerror.New("Gemini 授权已换票但未能保存会话，请重新登录")
		}
	}
	id, err := s.saveGeminiCredential(ctx, cfg, channel.Id, *session.Credential)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	result := ChannelLoginResult{Status: "completed", CredentialID: id}
	session.Result, session.Credential = &result, nil
	if err = s.saveGeminiLogin(ctx, cfg, state, session); err != nil {
		return ChannelLoginResult{}, gerror.New("Gemini 凭据已保存，但登录结果缓存失败；请刷新凭据列表")
	}
	return result, nil
}

func (s *sChannel) exchangeGeminiLogin(ctx context.Context, cfg geminiOAuthConfig, channel entity.Channels, stateKey string, session *geminiLoginSession, cb geminiLoginCallback) error {
	if cb.Denied {
		session.Failed, session.Verifier = true, ""
		return gerror.New("Google 授权已取消或被拒绝，请重新发起登录")
	}
	client, err := s.GeminiSubscriptionHTTPClient(ctx, channel, 0)
	if err != nil {
		return err
	}
	// 后端使用一次性标记；即使进程在 Google 消费验证码后退出，也不重复换票。
	consumeTTL := time.Until(session.ExpiresAt)
	if consumeTTL <= 0 {
		return gerror.New("Gemini 登录会话已过期")
	}
	consumed, err := s.app.Redis.SetNX(ctx, stateKey+":consumed", "1", consumeTTL).Result()
	if err != nil || !consumed {
		session.Failed, session.Verifier = true, ""
		return gerror.New("Gemini 授权码已处理或暂时不可用，请重新登录")
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "redirect_uri": {session.RedirectURI},
		"code": {cb.Code}, "code_verifier": {session.Verifier},
	}
	session.Failed, session.Verifier = true, ""
	tokens, err := exchangeGeminiOAuthTokens(ctx, client, cfg, form)
	if err != nil {
		return err
	}
	if tokens.RefreshToken == "" {
		return gerror.New("Google 未返回离线刷新令牌，请重新授权")
	}
	expiry := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second).Unix()
	subject, err := geminiOAuthSubject(ctx, client, tokens.AccessToken)
	if err != nil {
		return err
	}
	session.Failed = false
	session.Credential = &geminiSubscriptionCredential{Subject: subject, AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresAt: expiry}
	return nil
}
