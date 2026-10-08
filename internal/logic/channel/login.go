package channel

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// pendingLoginCode 是平台登录接口在「用户尚未完成登录」时返回的业务码。
const pendingLoginCode = 11217

// ChannelLoginSession 是一次外链登录会话：AuthURL 交给管理员在浏览器里完成登录
// （微信扫码、账号密码、企业 SSO 都由平台登录页决定），State 用于随后轮询换票。
type ChannelLoginSession struct {
	State   string
	AuthURL string
}

// ChannelLoginResult 是一次登录轮询的结果。Status 为 pending 表示还在等待用户
// 完成登录；completed 表示已拿到令牌并写入渠道凭据。
type ChannelLoginResult struct {
	Status       string
	CredentialID uint64
	UID          string
}

// StartChannelLogin 向渠道类型声明的登录接口申请一次性登录地址。服务端只负责取地址
// 与轮询换票，登录动作在平台自己的页面上完成，因此不依赖登录所在设备的本地组件
// （这正是桌面端 native 模块无法在服务端复现、而外链登录可以的原因）。
func (s *sChannel) StartChannelLogin(ctx context.Context, channelID uint64) (ChannelLoginSession, error) {
	channel, config, err := s.loginContext(ctx, channelID)
	if err != nil {
		return ChannelLoginSession{}, err
	}
	endpoint, err := loginEndpoint(channel.BaseUrl, config, "auth/state")
	if err != nil {
		return ChannelLoginSession{}, err
	}
	query := url.Values{}
	query.Set("platform", config.Login.Platform)

	body, err := s.fetchLoginJSON(ctx, channel, http.MethodPost, endpoint+"?"+query.Encode(), "")
	if err != nil {
		return ChannelLoginSession{}, err
	}
	if code := gjson.GetBytes(body, "code").Int(); code != 0 {
		return ChannelLoginSession{}, gerror.Newf("申请登录地址失败：%s", loginUpstreamMessage(body))
	}
	session := ChannelLoginSession{
		State:   strings.TrimSpace(gjson.GetBytes(body, "data.state").String()),
		AuthURL: strings.TrimSpace(gjson.GetBytes(body, "data.authUrl").String()),
	}
	if session.State == "" || session.AuthURL == "" {
		return ChannelLoginSession{}, gerror.New("登录接口未返回 state 或 authUrl")
	}
	return session, nil
}

// PollChannelLogin 轮询一次登录结果；拿到令牌后立即写入渠道凭据并返回新凭据 id。
func (s *sChannel) PollChannelLogin(ctx context.Context, channelID uint64, state string) (ChannelLoginResult, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return ChannelLoginResult{}, gerror.New("登录 state 不能为空")
	}
	channel, config, err := s.loginContext(ctx, channelID)
	if err != nil {
		return ChannelLoginResult{}, err
	}
	endpoint, err := loginEndpoint(channel.BaseUrl, config, "auth/token")
	if err != nil {
		return ChannelLoginResult{}, err
	}
	query := url.Values{}
	query.Set("state", state)

	body, err := s.fetchLoginJSON(ctx, channel, http.MethodGet, endpoint+"?"+query.Encode(), "")
	if err != nil {
		return ChannelLoginResult{}, err
	}
	switch code := gjson.GetBytes(body, "code").Int(); code {
	case pendingLoginCode:
		return ChannelLoginResult{Status: "pending"}, nil
	case 0:
	default:
		return ChannelLoginResult{}, gerror.Newf("登录失败：%s", loginUpstreamMessage(body))
	}
	token := strings.TrimSpace(gjson.GetBytes(body, "data.accessToken").String())
	if token == "" {
		return ChannelLoginResult{}, gerror.New("登录接口未返回 accessToken")
	}
	uid := s.loginAccountUID(ctx, channel, config, state, token)
	credentialID, err := s.CreateCredential(ctx, channelID, adminapi.ChannelCredentialInput{APIKey: token})
	if err != nil {
		return ChannelLoginResult{}, err
	}
	return ChannelLoginResult{Status: "completed", CredentialID: credentialID, UID: uid}, nil
}

// loginContext 读取渠道与其类型配置，并确认该类型声明了外链登录能力。
func (s *sChannel) loginContext(ctx context.Context, channelID uint64) (entity.Channels, channeltype.Config, error) {
	channel, err := s.GetOwned(ctx, channelID)
	if err != nil {
		return entity.Channels{}, channeltype.Config{}, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return entity.Channels{}, channeltype.Config{}, err
	}
	if config.Login.Adapter != channeltype.AdapterLoginExternalLink {
		return entity.Channels{}, channeltype.Config{}, gerror.Newf("渠道类型 %s 不支持登录，请手工填写密钥", channel.Type)
	}
	return channel, config, nil
}

// loginEndpoint 拼出登录接口地址。渠道根地址本身带版本前缀（WorkBuddy 是
// https://www.workbuddy.cn/v2），而登录接口固定在 /v2 之下，因此取根地址的
// scheme+host，再拼 /v2 + prefixPath + 动作。
func loginEndpoint(baseURL string, config channeltype.Config, action string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", gerror.New("渠道根地址不是可用的 HTTP(S) 地址")
	}
	prefix := strings.TrimSuffix(config.Login.PrefixPath, "/")
	return parsed.Scheme + "://" + parsed.Host + "/v2" + prefix + "/" + action, nil
}

// fetchLoginJSON 请求平台登录接口。登录期没有可用于该平台的凭据，因此显式带上
// X-No-* 头关闭平台自身的鉴权注入；token 非空时用于取回登录账号信息。
func (s *sChannel) fetchLoginJSON(ctx context.Context, channel entity.Channels, method, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, gerror.Wrap(err, "创建登录请求失败")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-No-Authorization", "true")
	req.Header.Set("X-No-User-Id", "true")
	req.Header.Set("X-No-Enterprise-Id", "true")
	req.Header.Set("X-No-Department-Info", "true")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client, err := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if err != nil {
		return nil, err
	}
	return s.fetchJSON(client, req, upstreamJSONRequest{
		Method: method, Endpoint: endpoint, BodyLimit: 1 << 20,
		RequestError: "创建登录请求失败",
		FetchError:   "请求登录接口失败",
		ReadError:    "读取登录接口响应失败",
		InvalidError: "登录接口返回了无效 JSON",
	})
}

// loginAccountUID 尽力取回登录账号的 uid；失败不阻塞凭据创建（推理调用只需要令牌）。
func (s *sChannel) loginAccountUID(ctx context.Context, channel entity.Channels, config channeltype.Config, state, token string) string {
	endpoint, err := loginEndpoint(channel.BaseUrl, config, "login/account")
	if err != nil {
		return ""
	}
	query := url.Values{}
	query.Set("state", state)
	body, err := s.fetchLoginJSON(ctx, channel, http.MethodGet, endpoint+"?"+query.Encode(), token)
	if err != nil {
		return ""
	}
	for _, path := range []string{"data.uid", "data.account.uid"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func loginUpstreamMessage(body []byte) string {
	message := strings.TrimSpace(gjson.GetBytes(body, "msg").String())
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}
