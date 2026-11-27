package relay

import (
	"context"
	"net/http"

	"github.com/yunloli/aiferry/internal/logic/channel"
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
//   - 配置渠道代理时始终按代理计划执行，DirectHTTP 不得绕过配置；
//   - DirectHTTP（视频文件下载等）仅在未配置代理时使用 HTTPDirect；
//   - 其余按渠道代理配置 + 密钥固定序号模运算配对（2 代理 5 密钥：
//     1-1,2-2,3-1,4-2,5-1），代理层失败仅当前这把密钥顺延下一个代理，
//     不随机、不影响其它密钥的映射，详见 channel.DoViaProxies。
//
// do 内通过 GetBody 回卷请求体，可安全地对同一 req 重试。
func (s *sRelay) doViaProxy(ctx context.Context, req *http.Request, candidate Candidate, do func(*http.Client) (*http.Response, error)) (*http.Response, error) {
	// 路由缓存和异步任务中的代理快照可能过期；真实渠道每次出站前
	// 强制读取当前策略，读取失败绝不能使用旧值或降级直连。
	if candidate.ChannelID > 0 {
		current, err := s.channels.CurrentProxyCipher(ctx, candidate.ChannelID)
		if err != nil {
			return nil, err
		}
		candidate.ProxyURLCipher = current
	}
	if candidate.ChannelType == "codex" {
		originalDo := do
		do = func(client *http.Client) (*http.Response, error) {
			return originalDo(channel.CodexHTTPClient(client))
		}
	}
	if candidate.DirectHTTP && candidate.ProxyURLCipher == "" {
		return do(s.app.HTTPDirect)
	}
	proxies, err := s.channels.ProxiesForCredential(ctx, candidate.ProxyURLCipher, candidate.ChannelID, candidate.ChannelCredentialID)
	if err != nil {
		return nil, err
	}
	// Antigravity 桥接自己不出网，真正的出口是容器里的官方 agy 进程；把该密钥
	// 当前选中的代理地址用内部请求头交给桥接，桥接据此给 agy 设置代理环境，
	// 使「密钥 ↔ 代理」配对在桥接链路上同样成立（与直连渠道行为一致）。
	// 该头只在令牌认证通过后由桥接读取，且不会出现在桥接的任何响应里。
	// 写在 do 闭包内而不是提前写死：DoViaProxies 代理层失败顺延下一个代理
	// 重试时，头里必须是顺延后的地址，否则桥接仍会拿失效的代理去打。
	if channel.IsAntigravityChannel(candidate.ChannelType) {
		originalDo := do
		do = func(client *http.Client) (*http.Response, error) {
			req.Header.Del(channel.AntigravityProxyHeader)
			if current := proxies.Current(); current != "" {
				req.Header.Set(channel.AntigravityProxyHeader, current)
			}
			return originalDo(client)
		}
	}
	return s.channels.DoViaProxies(proxies, resetRequestBody(req), do)
}
