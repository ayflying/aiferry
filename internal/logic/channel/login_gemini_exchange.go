package channel

import (
	"context"
	"net/url"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) exchangeGeminiLogin(ctx context.Context, cfg geminiOAuthConfig, channel entity.Channels, stateKey string, session *geminiLoginSession, cb geminiLoginCallback) error {
	if cb.Denied {
		session.Failed, session.Verifier = true, ""
		if session.ProjectPicker {
			return gerror.New("Google 授权已取消或被拒绝；也可手动输入项目 ID 后重新授权")
		}
		return gerror.New("Google 授权已取消或被拒绝，请重新发起登录")
	}
	client, err := s.GeminiSubscriptionHTTPClient(ctx, channel, session.TargetID)
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
	if session.ProjectPicker {
		session.Projects, session.ProjectMessage = geminiProjectChoices(ctx, client, tokens)
	}
	session.Failed = false
	session.Credential = &geminiSubscriptionCredential{Subject: subject, ProjectID: session.ProjectID, AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresAt: expiry}
	return nil
}
