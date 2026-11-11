package channel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type codexLoginState struct {
	ChannelID    uint64              `json:"channelId"`
	UserID       uint64              `json:"userId"`
	DeviceAuthID string              `json:"deviceAuthId"`
	UserCode     string              `json:"userCode"`
	Interval     int                 `json:"interval"`
	ExpiresAt    int64               `json:"expiresAt"`
	Result       *ChannelLoginResult `json:"result,omitempty"`
	Credential   *codexCredential    `json:"credential,omitempty"`
}

func randomCodexState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", gerror.Wrap(err, "生成授权会话失败")
	}
	return hex.EncodeToString(b), nil
}

func (s *sChannel) startCodexLogin(ctx context.Context, channel entity.Channels) (ChannelLoginSession, error) {
	if s.app.Redis == nil {
		return ChannelLoginSession{}, gerror.New("登录授权需要可用的会话缓存")
	}
	current, ok := auth.CurrentUser(ctx)
	if !ok {
		return ChannelLoginSession{}, gerror.New("未登录")
	}
	client, err := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if err != nil {
		return ChannelLoginSession{}, err
	}
	var reply struct {
		DeviceAuthID string       `json:"device_auth_id"`
		UserCode     string       `json:"user_code"`
		Usercode     string       `json:"usercode"`
		Interval     codexSeconds `json:"interval"`
	}
	payload, _ := json.Marshal(map[string]string{"client_id": codexClientID})
	status, err := codexAuthRequest(ctx, client, "/api/accounts/deviceauth/usercode", "application/json", string(payload), &reply)
	if err != nil {
		return ChannelLoginSession{}, err
	}
	if status != http.StatusOK {
		return ChannelLoginSession{}, gerror.Newf("申请 Codex 设备授权失败（HTTP %d）；请确认账号允许设备码登录", status)
	}
	if reply.UserCode == "" {
		reply.UserCode = reply.Usercode
	}
	if reply.DeviceAuthID == "" || reply.UserCode == "" {
		return ChannelLoginSession{}, gerror.New("Codex 未返回设备授权信息")
	}
	interval := max(5, int(reply.Interval))
	state, err := randomCodexState()
	if err != nil {
		return ChannelLoginSession{}, err
	}
	session := codexLoginState{ChannelID: channel.Id, UserID: current.Id, DeviceAuthID: reply.DeviceAuthID, UserCode: reply.UserCode, Interval: interval, ExpiresAt: time.Now().Add(codexLoginTTL).Unix()}
	if err = s.saveCodexLogin(ctx, state, session); err != nil {
		return ChannelLoginSession{}, err
	}
	return ChannelLoginSession{State: state, AuthURL: codexIssuer + "/codex/device", UserCode: reply.UserCode, IntervalSeconds: interval, ExpiresInSeconds: int(codexLoginTTL.Seconds())}, nil
}

func (s *sChannel) saveCodexLogin(ctx context.Context, state string, session codexLoginState) error {
	body, err := json.Marshal(session)
	if err != nil {
		return gerror.Wrap(err, "编码授权会话失败")
	}
	cipher, err := s.app.Secrets.Encrypt(string(body))
	if err != nil {
		return err
	}
	ttl := time.Until(time.Unix(session.ExpiresAt, 0))
	if ttl <= 0 {
		return gerror.New("Codex 登录授权已过期，请重新发起")
	}
	return s.app.Redis.Set(ctx, "aiferry:codex-login:"+state, cipher, ttl).Err()
}

func (s *sChannel) pollCodexLogin(ctx context.Context, channel entity.Channels, state string) (ChannelLoginResult, error) {
	if s.app.Redis == nil {
		return ChannelLoginResult{}, gerror.New("登录会话缓存不可用")
	}
	current, ok := auth.CurrentUser(ctx)
	if !ok {
		return ChannelLoginResult{}, gerror.New("未登录")
	}
	cipher, err := s.app.Redis.Get(ctx, "aiferry:codex-login:"+state).Result()
	if err != nil {
		return ChannelLoginResult{}, gerror.New("Codex 登录会话不存在或已过期，请重新发起")
	}
	plain, err := s.app.Secrets.Decrypt(cipher)
	if err != nil {
		return ChannelLoginResult{}, gerror.New("读取 Codex 登录会话失败")
	}
	var session codexLoginState
	if json.Unmarshal([]byte(plain), &session) != nil || session.ChannelID != channel.Id || session.UserID != current.Id || session.ExpiresAt <= time.Now().Unix() {
		return ChannelLoginResult{}, gerror.New("Codex 登录会话无效或已过期")
	}
	if session.Result != nil {
		return *session.Result, nil
	}
	// NX 同时保证一次性换票互斥与官方轮询频率；不允许不同浏览器重复消费授权码。
	gate := fmt.Sprintf("aiferry:codex-login-poll:%s", state)
	acquired, err := s.app.Redis.SetNX(ctx, gate, "1", codexLoginTTL).Result()
	if err != nil {
		return ChannelLoginResult{}, gerror.New("锁定 Codex 登录会话失败")
	}
	if !acquired {
		return ChannelLoginResult{Status: "pending"}, nil
	}
	// 获锁后重读，避免等待锁期间另一请求已完成、却仍消费旧快照的一次性票据。
	cipher, err = s.app.Redis.Get(ctx, "aiferry:codex-login:"+state).Result()
	if err != nil {
		return ChannelLoginResult{}, gerror.New("读取 Codex 登录会话失败")
	}
	plain, err = s.app.Secrets.Decrypt(cipher)
	if err != nil || json.Unmarshal([]byte(plain), &session) != nil {
		return ChannelLoginResult{}, gerror.New("读取 Codex 登录会话失败")
	}
	if session.Result != nil {
		return *session.Result, nil
	}
	if session.Credential != nil {
		return s.completeCodexLogin(ctx, state, session)
	}
	client, err := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	payload, _ := json.Marshal(map[string]string{"device_auth_id": session.DeviceAuthID, "user_code": session.UserCode})
	var reply struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	status, err := codexAuthRequest(ctx, client, "/api/accounts/deviceauth/token", "application/json", string(payload), &reply)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	if status == http.StatusForbidden || status == http.StatusNotFound {
		_ = s.app.Redis.Expire(ctx, gate, time.Duration(session.Interval)*time.Second).Err()
		return ChannelLoginResult{Status: "pending"}, nil
	}
	if status != http.StatusOK || reply.AuthorizationCode == "" || reply.CodeVerifier == "" {
		return ChannelLoginResult{}, gerror.Newf("Codex 设备授权失败（HTTP %d），请重新发起登录", status)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {codexClientID}, "code": {reply.AuthorizationCode}, "code_verifier": {reply.CodeVerifier}, "redirect_uri": {codexIssuer + "/deviceauth/callback"}}
	tokens, err := exchangeCodexTokens(ctx, client, form)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	credential, err := newCodexCredential(tokens)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	session.Credential = &credential
	// 换票完成后先保存加密的待入库结果；数据库暂时失败时可重试而不再消费授权码。
	if err = s.saveCodexLogin(ctx, state, session); err != nil {
		return ChannelLoginResult{}, gerror.New("登录票据已消费但会话保存失败，请重新登录")
	}
	return s.completeCodexLogin(ctx, state, session)
}

func (s *sChannel) completeCodexLogin(ctx context.Context, state string, session codexLoginState) (ChannelLoginResult, error) {
	id, err := s.createCodexCredential(ctx, session.ChannelID, *session.Credential)
	if err != nil {
		_ = s.app.Redis.Expire(ctx, "aiferry:codex-login-poll:"+state, time.Duration(session.Interval)*time.Second).Err()
		return ChannelLoginResult{}, err
	}
	result := ChannelLoginResult{Status: "completed", CredentialID: id, UID: session.Credential.AccountID}
	session.Result = &result
	session.DeviceAuthID = ""
	session.Credential = nil
	// 保存完成结果实现幂等；写入失败也不向调用方回显令牌或重复换票。
	if err = s.saveCodexLogin(ctx, state, session); err != nil {
		_ = s.app.Redis.Expire(ctx, "aiferry:codex-login-poll:"+state, time.Duration(session.Interval)*time.Second).Err()
		return ChannelLoginResult{}, gerror.New("凭据已保存，但登录结果缓存失败；请刷新凭据列表")
	}
	return result, nil
}
