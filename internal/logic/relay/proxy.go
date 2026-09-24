package relay

import (
	"context"
	"net/http"
)

// resetRequestBody 返回一个在每次重试前把 req.Body 回卷到起点的函数。
// http.NewRequest 对 *bytes.Reader 会自动挂上 GetBody，传输失败重试时上一次
// 可能已消费 Body，不回卷会让下一个代理收到空请求体。
func resetRequestBody(req *http.Request) func() error {
	return func() error {
		if req.GetBody == nil {
			return nil
		}
		bodyCopy, err := req.GetBody()
		if err != nil {
			return err
		}
		req.Body = bodyCopy
		return nil
	}
}

// doViaProxy 经渠道代理执行一次上游请求，是 relay 各分支统一的代理入口：
//   - DirectHTTP（视频文件下载等）强制直连，不参与密钥-代理配对；
//   - 其余按渠道代理配置 + 密钥固定序号模运算配对（2 代理 5 密钥：
//     1-1,2-2,3-1,4-2,5-1），代理层失败仅当前这把密钥顺延下一个代理，
//     不随机、不影响其它密钥的映射，详见 channel.DoViaProxies。
//
// do 内通过 GetBody 回卷请求体，可安全地对同一 req 重试。
func (s *sRelay) doViaProxy(ctx context.Context, req *http.Request, candidate Candidate, do func(*http.Client) (*http.Response, error)) (*http.Response, error) {
	if candidate.DirectHTTP {
		return do(s.app.HTTPDirect)
	}
	proxies, err := s.channels.ProxiesForCredential(ctx, candidate.ProxyURLCipher, candidate.ChannelID, candidate.ChannelCredentialID)
	if err != nil {
		return nil, err
	}
	return s.channels.DoViaProxies(proxies, resetRequestBody(req), do)
}
