package channel

import (
	"context"
	"io"
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/model/entity"
)

type upstreamJSONRequest struct {
	Method       string
	Endpoint     string
	AuthType     string
	HeaderName   string
	HeaderPrefix string
	BodyLimit    int64
	// SkipClientIdentity 为真时不注入渠道的客户端身份头。少数上游路由
	// （如 WorkBuddy 官网额度接口）属于网页会话域，chat 身份头反而会被
	// 会话门当成跨站请求拒绝，需要它们自己指定更"浏览器"的请求特征。
	SkipClientIdentity bool
	// Headers 是在鉴权头与身份头之后追加的请求头，优先级最高。
	Headers      http.Header
	RequestError string
	FetchError   string
	ReadError    string
	InvalidError string
	StatusError  func(status int, body []byte) error
}

func (s *sChannel) fetchUpstreamJSON(ctx context.Context, channel entity.Channels, credentialCipher string, input upstreamJSONRequest) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, input.Method, input.Endpoint, nil)
	if err != nil {
		return nil, gerror.Wrap(err, input.RequestError)
	}
	if err = s.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{
		Spec: UpstreamAuthSpec{
			AuthType:     input.AuthType,
			HeaderName:   input.HeaderName,
			HeaderPrefix: input.HeaderPrefix,
		},
		CredentialCipher:    credentialCipher,
		ManagementKeyCipher: channel.ManagementKeyCipher,
		OrganizationID:      channel.OrganizationId,
		ProjectID:           channel.ProjectId,
	}); err != nil {
		return nil, err
	}
	// OpenCode Go 等上游要求稳定的客户端标识头，模型同步与额度查询同样需要。
	// WorkBuddy 的网页额度路由例外：见 SkipClientIdentity 的说明。
	if !input.SkipClientIdentity {
		ApplyUpstreamClientHeaders(req.Header, nil, UpstreamClientIdentity{
			ChannelType: channel.Type,
			ChannelID:   channel.Id,
			BaseURL:     channel.BaseUrl,
		})
	}
	// 调用方显式指定的请求头最后写入，覆盖前面按渠道派生的默认值。
	for name, values := range input.Headers {
		req.Header.Del(name)
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	client, err := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if err != nil {
		return nil, err
	}
	return s.fetchJSON(client, req, input)
}

func (s *sChannel) fetchPublicJSON(ctx context.Context, input upstreamJSONRequest) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, input.Method, input.Endpoint, nil)
	if err != nil {
		return nil, gerror.Wrap(err, input.RequestError)
	}
	req.Header.Set("Accept", "application/json")
	return s.fetchJSON(s.app.HTTP, req, input)
}

func (s *sChannel) fetchJSON(client *http.Client, req *http.Request, input upstreamJSONRequest) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, input.FetchError)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, input.BodyLimit))
	if err != nil {
		return nil, gerror.Wrap(err, input.ReadError)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if input.StatusError != nil {
			return nil, input.StatusError(resp.StatusCode, body)
		}
		return nil, gerror.Newf("upstream request returned HTTP %d", resp.StatusCode)
	}
	if !gjson.ValidBytes(body) {
		return nil, gerror.New(input.InvalidError)
	}
	return body, nil
}
