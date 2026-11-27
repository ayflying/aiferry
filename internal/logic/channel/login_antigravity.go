package channel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// Antigravity 桥接自管 OAuth 的登录流程。
//
// 与 external_link 的区别：授权发生在 Google 页面上，回调落在管理员本机
// （官方客户端注册的固定回环地址 http://localhost:51121/oauth-callback），
// 桥接容器收不到这个回调，所以没有「服务端轮询换票」这一步——必须由管理员
// 把浏览器地址栏里的回调地址粘回平台，再转交桥接完成换票。
//
// 账号身份就是桥接令牌本身：用哪个账号的令牌发起登录，登录态就写进那个账号的
// HOME 目录。因此这里不需要额外的「登录到哪个账号」参数。

// antigravityLoginEndpoint 拼出桥接的登录接口地址。桥接根地址已带 /v1 前缀，
// 与模型、额度端点同一套规则。
func antigravityLoginEndpoint(baseURL, action string) (string, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return "", gerror.New("渠道根地址不能为空")
	}
	return strings.TrimSuffix(trimmed, "/") + "/login/" + action, nil
}

// antigravityBridgeToken 取该渠道的一把桥接令牌。令牌既是调用桥接的凭据，
// 也是「给哪个 Google 账号登录」的声明。credentialID 非零时精确取点选的那一行，
// 保证多账号下「重新登录」不会作用到别的账号；为零且渠道还没有任何令牌时，
// 自动生成一把——用户点「平台登录」即可，不需要先手工准备令牌。
func (s *sChannel) antigravityBridgeToken(ctx context.Context, channelID, credentialID uint64, provision bool) (string, error) {
	rows, err := s.availableCredentials(ctx, channelID, nil)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		// 发起登录时允许自动补一把；提交回调时不允许——那时必须沿用发起时的
		// 那把令牌，否则桥接会拿着新账号找不到这次授权的 state。
		if !provision {
			return "", gerror.New("该渠道已没有可用的桥接令牌，请重新发起登录")
		}
		if credentialID != 0 {
			return "", gerror.New("指定的凭据不存在或当前不可用，请刷新后重试")
		}
		return s.provisionAntigravityToken(ctx, channelID)
	}
	selected := rows[0]
	if credentialID != 0 {
		matched := false
		for _, row := range rows {
			if row.Id == credentialID {
				selected, matched = row, true
				break
			}
		}
		if !matched {
			return "", gerror.New("指定的凭据不存在或当前不可用，请刷新后重试")
		}
	}
	plain, err := s.app.Secrets.Decrypt(selected.ApiKeyCipher)
	if err != nil {
		return "", gerror.New("读取桥接令牌失败")
	}
	token := strings.TrimSpace(plain)
	if token == "" {
		return "", gerror.New("该渠道的桥接令牌为空，请重新填写")
	}
	return token, nil
}

// provisionAntigravityToken 给还没有任何令牌的渠道自动生成一把桥接令牌并落库。
//
// 桥接开启 AGY_BRIDGE_AUTO_ACCOUNT 后，会在第一次收到这把令牌调 /v1/login/start 时
// 自动登记账号，所以「新建渠道 → 点平台登录 → 网页授权」全程不需要用户接触令牌，
// 也不需要管理员去服务器上找卷里的令牌再粘回来。
func (s *sChannel) provisionAntigravityToken(ctx context.Context, channelID uint64) (string, error) {
	token, err := randomBridgeToken()
	if err != nil {
		return "", gerror.Wrap(err, "生成桥接令牌失败")
	}
	if _, err = s.CreateCredential(ctx, channelID, adminapi.ChannelCredentialInput{
		APIKey: token,
		Remark: "网页授权自动创建",
	}); err != nil {
		return "", gerror.Wrap(err, "写入桥接令牌失败")
	}
	return token, nil
}

// randomBridgeToken 生成 32 字节随机令牌（64 位十六进制），满足桥接「至少 32 字节」的要求。
func randomBridgeToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// callAntigravityBridge 调用桥接的登录端点。代理走该渠道已有配置，
// 与对话、额度请求保持一致（桥接访问 Google 需要同一条出口）。
func (s *sChannel) callAntigravityBridge(ctx context.Context, channel entity.Channels, action string, body []byte, token string) ([]byte, error) {
	endpoint, err := antigravityLoginEndpoint(channel.BaseUrl, action)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, gerror.Wrap(err, "创建桥接登录请求失败")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client, err := s.HTTPClientForChannel(ctx, channel)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, gerror.Wrap(err, "请求桥接登录接口失败")
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, gerror.Wrap(err, "读取桥接登录响应失败")
	}
	if response.StatusCode != http.StatusOK {
		return nil, gerror.Newf("桥接登录失败：%s", bridgeLoginMessage(payload))
	}
	return payload, nil
}

// bridgeLoginMessage 从桥接的错误响应里取可读信息；取不到时退回截断后的正文。
func bridgeLoginMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if message := strings.TrimSpace(payload.Error.Message); message != "" {
			return message
		}
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		return "桥接返回了空响应"
	}
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

// startAntigravityBridgeLogin 让桥接生成一次授权，返回管理员要在浏览器打开的地址。
func (s *sChannel) startAntigravityBridgeLogin(ctx context.Context, channel entity.Channels, credentialID uint64) (ChannelLoginSession, error) {
	token, err := s.antigravityBridgeToken(ctx, channel.Id, credentialID, true)
	if err != nil {
		return ChannelLoginSession{}, err
	}
	body, err := s.callAntigravityBridge(ctx, channel, "start", []byte("{}"), token)
	if err != nil {
		return ChannelLoginSession{}, err
	}
	var payload struct {
		State     string `json:"state"`
		AuthURL   string `json:"authUrl"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ChannelLoginSession{}, gerror.New("桥接登录接口返回了无效 JSON")
	}
	if strings.TrimSpace(payload.State) == "" || strings.TrimSpace(payload.AuthURL) == "" {
		return ChannelLoginSession{}, gerror.New("桥接未返回登录状态或授权地址")
	}
	return ChannelLoginSession{State: payload.State, AuthURL: payload.AuthURL, ExpiresInSeconds: payload.ExpiresIn}, nil
}

// completeAntigravityBridgeLogin 把管理员粘回来的回调地址转交桥接完成换票。
func (s *sChannel) completeAntigravityBridgeLogin(ctx context.Context, channel entity.Channels, credentialID uint64, state, callbackURL string) (ChannelLoginResult, error) {
	token, err := s.antigravityBridgeToken(ctx, channel.Id, credentialID, false)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	requestBody, err := json.Marshal(map[string]string{"state": strings.TrimSpace(state), "input": strings.TrimSpace(callbackURL)})
	if err != nil {
		return ChannelLoginResult{}, gerror.Wrap(err, "编码登录请求失败")
	}
	body, err := s.callAntigravityBridge(ctx, channel, "complete", requestBody, token)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	var payload struct {
		Email     string `json:"email"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ChannelLoginResult{}, gerror.New("桥接登录接口返回了无效 JSON")
	}
	// 登录态写进了桥接卷，桥接令牌本来就在渠道里，不需要再新增凭据。
	return ChannelLoginResult{Status: "completed", UID: payload.Email}, nil
}

// CompleteChannelLogin 提交授权回调地址，完成一次桥接自管的登录。
// 只有声明了 antigravity_bridge 适配器的渠道类型可以调用。
func (s *sChannel) CompleteChannelLogin(ctx context.Context, channelID uint64, input adminapi.ChannelLoginCompleteInput) (ChannelLoginResult, error) {
	channel, config, err := s.loginContext(ctx, channelID)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	if config.Login.Adapter != channeltype.AdapterLoginAntigravityBridge {
		return ChannelLoginResult{}, gerror.New("该渠道类型不支持提交授权回调地址")
	}
	var credentialID uint64
	if input.CredentialID != nil {
		credentialID = *input.CredentialID
	}
	return s.completeAntigravityBridgeLogin(ctx, channel, credentialID, input.State, input.CallbackURL)
}
