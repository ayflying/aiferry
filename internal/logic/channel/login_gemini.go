package channel

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const geminiLoginTTL = 10 * time.Minute

type geminiLoginSession struct {
	ChannelID      uint64
	UserID         uint64
	TargetID       uint64
	ProjectID      string
	ProjectPicker  bool
	Projects       []adminapi.ChannelLoginProject
	ProjectMessage string
	ExpiresAt      time.Time
	RedirectURI    string
	Verifier       string
	Credential     *geminiSubscriptionCredential
	Result         *ChannelLoginResult
	Failed         bool
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

func (s *sChannel) startGeminiLogin(ctx context.Context, channel entity.Channels, targetID uint64, project string) (ChannelLoginSession, error) {
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
	if targetID == 0 {
		if project != "" && !geminiProjectIDPattern.MatchString(project) {
			return ChannelLoginSession{}, gerror.New("新增 Gemini 账号需要有效的项目 ID")
		}
	} else {
		_, current, readErr := s.readGeminiLoginTarget(ctx, cfg, channel.Id, targetID)
		if readErr != nil {
			return ChannelLoginSession{}, readErr
		}
		project = geminiCredentialProject(current, channel.ProjectId)
		if project == "" {
			return ChannelLoginSession{}, gerror.New("原 Gemini 凭据及渠道均未配置项目 ID，无法重新授权")
		}
	}
	if _, err = s.GeminiSubscriptionHTTPClient(ctx, channel, targetID); err != nil {
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
	session := geminiLoginSession{
		ChannelID: channel.Id, UserID: user.Id, TargetID: targetID, ProjectID: project,
		ProjectPicker: targetID == 0 && project == "", ExpiresAt: time.Now().Add(geminiLoginTTL),
		RedirectURI: cfg.redirectURI, Verifier: verifier,
	}
	if err = s.saveGeminiLogin(ctx, cfg, state, session); err != nil {
		return ChannelLoginSession{}, err
	}
	return ChannelLoginSession{State: state, AuthURL: cfg.authorizationURL(state, challenge, session.ProjectPicker), IntervalSeconds: 3, ExpiresInSeconds: int(geminiLoginTTL.Seconds())}, nil
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

func (s *sChannel) pollGeminiLogin(ctx context.Context, channel entity.Channels, state, project string) (ChannelLoginResult, error) {
	user, ok := auth.CurrentUser(ctx)
	if !ok {
		return ChannelLoginResult{}, gerror.New("未登录")
	}
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		return ChannelLoginResult{}, err
	}
	readOwned := func(ctx context.Context) (geminiLoginSession, error) {
		session, err := s.readGeminiLogin(ctx, cfg, state)
		if err != nil || session.ChannelID != channel.Id || session.UserID != user.Id {
			return geminiLoginSession{}, gerror.New("Gemini 登录会话无效或已过期")
		}
		return session, nil
	}
	session, err := readOwned(ctx)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	if result, err := session.pollResult(project); result != nil || err != nil {
		return geminiPollReply(result, err)
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
	// 在前端 30 秒默认请求期限前，为缓存保存与释放租约保留时间。
	deadline := time.Now().Add(25 * time.Second)
	if session.ExpiresAt.Before(deadline) {
		deadline = session.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	lease, err := randomCodexState()
	if err != nil {
		return ChannelLoginResult{}, err
	}
	acquired, err := s.app.Redis.SetNX(ctx, key+":poll", lease, 45*time.Second).Result()
	if err != nil {
		return ChannelLoginResult{}, gerror.New("锁定 Gemini 登录会话失败")
	}
	if !acquired {
		return pending, nil
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = s.app.Redis.CompareAndDelete(cleanupCtx, key+":poll", lease).Err()
	}()
	// 获锁后重新检查归属、有效期和阶段，确保只换票一次且不能更换已确认的项目。
	session, err = readOwned(ctx)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	if result, err := session.pollResult(project); result != nil || err != nil {
		return geminiPollReply(result, err)
	}
	if session.Credential == nil {
		var cb geminiLoginCallback
		if err = cfg.open("callback:"+state, sealed, &cb); err != nil {
			return ChannelLoginResult{}, err
		}
		if err = s.exchangeGeminiLogin(ctx, cfg, channel, key, &session, cb); err != nil {
			if session.Failed {
				_ = s.finishGeminiLogin(ctx, cfg, state, session)
			}
			return ChannelLoginResult{}, err
		}
		if err = s.finishGeminiLogin(ctx, cfg, state, session); err != nil {
			return ChannelLoginResult{}, gerror.New("Gemini 授权已换票但未能保存会话，请重新登录")
		}
		// 处理授权回调的那次轮询只展示项目，即使调用方提前传了 projectId。
		if session.ProjectPicker {
			return session.projectRequired(), nil
		}
	}
	if session.ProjectPicker && session.ProjectID == "" {
		// 先保存确认后的项目，以便数据库暂时不可用时只重试同一个项目。
		session.ProjectID, session.Credential.ProjectID = project, project
		session.Projects, session.ProjectMessage = nil, ""
		if err = s.saveGeminiLogin(ctx, cfg, state, session); err != nil {
			return ChannelLoginResult{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return ChannelLoginResult{}, gerror.New("Gemini 登录确认超时，请检查会话后重试")
	}
	id, err := s.saveGeminiCredential(ctx, cfg, channel.Id, session.TargetID, *session.Credential)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	result := ChannelLoginResult{Status: "completed", CredentialID: id}
	session.Result, session.Credential = &result, nil
	if err = s.finishGeminiLogin(ctx, cfg, state, session); err != nil {
		return ChannelLoginResult{}, gerror.New("Gemini 凭据已保存，但登录结果缓存失败；请刷新凭据列表")
	}
	return result, nil
}

func (session geminiLoginSession) pollResult(project string) (*ChannelLoginResult, error) {
	if project != "" {
		if !session.ProjectPicker {
			return nil, gerror.New("该登录会话不接受项目 ID")
		}
		if !geminiProjectIDPattern.MatchString(project) {
			return nil, gerror.New("新增 Gemini 账号需要有效的项目 ID")
		}
		if session.ProjectID != "" && project != session.ProjectID {
			return nil, gerror.New("本次登录已确认其他项目 ID，请勿更换项目")
		}
	}
	if session.Failed {
		if session.ProjectPicker {
			return nil, gerror.New("Gemini 授权未完成，请重试或手动输入项目 ID 后重新授权")
		}
		return nil, gerror.New("Gemini 授权未完成，请重新发起登录")
	}
	if session.Result != nil {
		return session.Result, nil
	}
	if session.ProjectPicker && session.Credential != nil && session.ProjectID == "" && project == "" {
		result := session.projectRequired()
		return &result, nil
	}
	return nil, nil
}

func geminiPollReply(result *ChannelLoginResult, err error) (ChannelLoginResult, error) {
	if err != nil {
		return ChannelLoginResult{}, err
	}
	return *result, nil
}

func (session geminiLoginSession) projectRequired() ChannelLoginResult {
	return ChannelLoginResult{Status: "project_required", Projects: session.Projects, ProjectMessage: session.ProjectMessage}
}

// 换票和凭据写入后即使客户端已断开，也尽量记录结果，不延长会话期限。
func (s *sChannel) finishGeminiLogin(ctx context.Context, cfg geminiOAuthConfig, state string, session geminiLoginSession) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.saveGeminiLogin(persistCtx, cfg, state, session)
}
