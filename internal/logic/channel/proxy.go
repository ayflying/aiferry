package channel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	appservice "github.com/yunloli/aiferry/internal/logic/app"
)

// proxyProbeURL 是测试代理连通性的探测目标。经代理拿到任意 HTTP 响应
// 即认为代理可用；407 表示代理认证失败，判定为不可用。
const proxyProbeURL = "http://www.gstatic.com/generate_204"

const proxyProbeTimeout = 10 * time.Second

func (s *sChannel) encryptProxyURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if _, err := appservice.NewProxyHTTPClient(s.app.HTTP, value); err != nil {
		return "", err
	}
	cipher, err := s.app.Secrets.Encrypt(value)
	return cipher, gerror.Wrap(err, "encrypt channel proxy URL")
}

func (s *sChannel) HTTPClientForProxy(proxyURLCipher string) (*http.Client, error) {
	if proxyURLCipher == "" {
		return s.app.HTTP, nil
	}
	proxyURL, err := s.app.Secrets.Decrypt(proxyURLCipher)
	if err != nil {
		return nil, gerror.Wrap(err, "decrypt channel proxy URL")
	}
	return appservice.NewProxyHTTPClient(s.app.HTTP, proxyURL)
}

// RevealProxyURL 解密并返回渠道代理地址明文，仅供管理端编辑回显。
// 未配置代理时返回空串；明文只经 HTTPS 响应下发，不落日志、不缓存。
func (s *sChannel) RevealProxyURL(ctx context.Context, id uint64) (string, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(row.ProxyUrlCipher) == "" {
		return "", nil
	}
	plainText, err := s.app.Secrets.Decrypt(row.ProxyUrlCipher)
	return plainText, gerror.Wrap(err, "decrypt channel proxy URL")
}

// ProxyTestResult 是代理连通性测试结果。业务失败（代理不通）不算接口错误，
// 经 Ok/Message 回传给前端弹框展示。
type ProxyTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

// TestProxy 用给定代理地址发一次探测请求，检验代理是否可用。
// 管理端「测试代理」按钮专用，不绑定渠道、不需要已保存。
func (s *sChannel) TestProxy(_ context.Context, proxyURL string) ProxyTestResult {
	return probeProxy(s.app.HTTP, proxyURL)
}

// probeProxy 构建代理客户端并探测 proxyProbeURL：
// 拿到任意 HTTP 响应即代理可用（附状态码）；407 为认证失败；
// 网络错误或代理地址格式非法为不可用。请求经代理发出，不泄漏给直连。
func probeProxy(base *http.Client, rawURL string) ProxyTestResult {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ProxyTestResult{OK: false, Message: "代理地址为空"}
	}
	client, err := appservice.NewProxyHTTPClient(base, rawURL)
	if err != nil {
		return ProxyTestResult{OK: false, Message: err.Error()}
	}
	probe := *client
	probe.Timeout = proxyProbeTimeout
	start := time.Now()
	resp, err := probe.Get(proxyProbeURL)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return ProxyTestResult{OK: false, LatencyMs: latency, Message: "代理连接失败：" + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusProxyAuthRequired {
		return ProxyTestResult{OK: false, LatencyMs: latency, Message: "代理认证失败（407），请检查代理账号密码"}
	}
	return ProxyTestResult{OK: true, LatencyMs: latency, Message: fmt.Sprintf("代理可用（HTTP %d）", resp.StatusCode)}
}
