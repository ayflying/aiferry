package channel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	appservice "github.com/yunloli/aiferry/internal/logic/app"
)

// proxyProbeURL 是测试代理连通性的探测目标。经代理拿到任意 HTTP 响应
// 即认为代理可用；407 表示代理认证失败，判定为不可用。
const proxyProbeURL = "http://www.gstatic.com/generate_204"

const proxyProbeTimeout = 10 * time.Second

// splitProxyLines 把多行代理配置文本拆成单行地址列表：
// 按换行拆分、去首尾空白、丢弃空行。单行历史数据（无换行）原样返回一行，
// 与旧版单代理存储完全兼容。
func splitProxyLines(plain string) []string {
	lines := make([]string, 0, 4)
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// ProxyIndexOf 按密钥固定序号（渠道内 #N，1-based）与代理总数做模运算，
// 得到该密钥的基线代理下标：2 个代理、5 把密钥 → 0,1,0,1,0（即 1-1,2-2,3-1,4-2,5-1）。
// count<=0 返回 -1（无代理）；ordinal 为 0（无密钥上下文）时固定取第一个代理。
func ProxyIndexOf(ordinal uint, count int) int {
	if count <= 0 {
		return -1
	}
	if ordinal == 0 {
		return 0
	}
	return int((ordinal - 1) % uint(count))
}

// CredentialProxies 是一把密钥在单次尝试序列内的代理轮换状态：
// 基线由密钥固定序号模运算决定；代理层失败时仅当前这把密钥顺延下一个
// （Advance），其它密钥的映射互不影响。轮换不跨请求持久化——基线始终固定。
type CredentialProxies struct {
	urls    []string
	current int
	tried   int
}

func newCredentialProxies(urls []string, ordinal uint) *CredentialProxies {
	p := &CredentialProxies{urls: urls}
	if len(urls) == 0 {
		return p
	}
	base := ProxyIndexOf(ordinal, len(urls))
	if base < 0 {
		base = 0
	}
	p.current = base
	return p
}

// Total 返回代理地址总数；0 表示该渠道未配置代理（直连）。
func (p *CredentialProxies) Total() int {
	if p == nil {
		return 0
	}
	return len(p.urls)
}

// Current 返回当前应使用的代理地址明文；空串表示直连。
func (p *CredentialProxies) Current() string {
	if p.Total() == 0 {
		return ""
	}
	return p.urls[p.current]
}

// Advance 把当前密钥顺延到下一个代理（列表内循环）。全部代理都已尝试过
// 后返回 false，调用方停止轮换、按最后一次结果收尾。
func (p *CredentialProxies) Advance() bool {
	if p.Total() == 0 {
		return false
	}
	p.tried++
	if p.tried >= len(p.urls) {
		return false
	}
	p.current = (p.current + 1) % len(p.urls)
	return true
}

func (s *sChannel) encryptProxyURL(value string) (string, error) {
	lines := splitProxyLines(value)
	if len(lines) == 0 {
		return "", gerror.New("代理地址为空")
	}
	for i, line := range lines {
		if _, err := appservice.NewProxyHTTPClient(s.app.HTTP, line); err != nil {
			return "", gerror.Newf("第 %d 行代理地址无效：%s", i+1, err.Error())
		}
	}
	cipher, err := s.app.Secrets.Encrypt(strings.Join(lines, "\n"))
	return cipher, gerror.Wrap(err, "encrypt channel proxy URL")
}

// ProxyURLs 解密渠道代理配置并拆成多行地址列表；未配置或解密结果为空行时
// 返回 nil（直连）。
func (s *sChannel) ProxyURLs(proxyURLCipher string) ([]string, error) {
	if proxyURLCipher == "" {
		return nil, nil
	}
	plainText, err := s.app.Secrets.Decrypt(proxyURLCipher)
	if err != nil {
		return nil, gerror.Wrap(err, "decrypt channel proxy URL")
	}
	lines := splitProxyLines(plainText)
	if len(lines) == 0 {
		return nil, nil
	}
	return lines, nil
}

// credentialProxyOrdinal 查询密钥在渠道内的固定序号（按 id 升序、含已软删
// 占位，与管理端「渠道 #N」同口径）。credentialID 为 0、查询失败或查不到时
// 回退 1（第一个代理），不阻断转发。
func (s *sChannel) credentialProxyOrdinal(ctx context.Context, channelID, credentialID uint64) uint {
	if credentialID == 0 {
		return 1
	}
	columns := dao.ChannelCredentials.Columns()
	count, err := dao.ChannelCredentials.Ctx(ctx).Unscoped().
		Where(columns.ChannelId, channelID).
		WhereLTE(columns.Id, credentialID).
		Count()
	if err != nil || count <= 0 {
		return 1
	}
	return uint(count)
}

// ProxiesForCredential 构造该密钥的代理轮换计划。渠道未配置代理时直接返回
// 空计划（直连）；只有 1 个代理时序号无关（模运算恒为 0），跳过序号查询——
// 单代理渠道（常见配置）在转发热路径上零额外查库，多代理才查固定序号做配对。
func (s *sChannel) ProxiesForCredential(ctx context.Context, proxyURLCipher string, channelID, credentialID uint64) (*CredentialProxies, error) {
	urls, err := s.ProxyURLs(proxyURLCipher)
	if err != nil {
		return nil, err
	}
	if len(urls) <= 1 {
		return newCredentialProxies(urls, 0), nil
	}
	return newCredentialProxies(urls, s.credentialProxyOrdinal(ctx, channelID, credentialID)), nil
}

// ClientFor 按轮换计划当前项构建 HTTP 客户端；计划为空（无代理）时直连。
func (s *sChannel) ClientFor(proxies *CredentialProxies) (*http.Client, error) {
	if proxies.Total() == 0 {
		return s.app.HTTP, nil
	}
	return appservice.NewProxyHTTPClient(s.app.HTTP, proxies.Current())
}

// HTTPClientForCredential 按密钥固定序号选定代理并构建客户端（不轮换）。
// 供管理端模型测试等「要如实反映该密钥真实出口」的诊断路径使用。
func (s *sChannel) HTTPClientForCredential(ctx context.Context, proxyURLCipher string, channelID, credentialID uint64) (*http.Client, error) {
	proxies, err := s.ProxiesForCredential(ctx, proxyURLCipher, channelID, credentialID)
	if err != nil {
		return nil, err
	}
	return s.ClientFor(proxies)
}

// DoViaProxies 经代理计划执行一次上游请求：
//   - 代理层失败（传输错误，或代理返回 407 认证失败）且计划里还有下一个代理时，
//     仅当前这把密钥的计划顺延一个代理重试——其它密钥各自的基线映射不受影响，
//     也不会随机换代理；
//   - 拿到非 407 的 HTTP 响应即视为代理可用（失败归上游处理），立即返回；
//   - resetBody 在每次调用 do 前重建请求体（重试时上一次可能已消费 Body）。
//
// 轮换状态只活在本次调用内：下次请求按密钥序号重新定位基线，固定映射不漂移。
func (s *sChannel) DoViaProxies(proxies *CredentialProxies, resetBody func() error, do func(*http.Client) (*http.Response, error)) (*http.Response, error) {
	return doViaProxies(proxies, s.ClientFor, resetBody, do)
}

// doViaProxies 是 DoViaProxies 的可测内核：clientFor 抽象出客户端构建，便于
// 单测注入假客户端与假传输，不依赖完整 sChannel。
func doViaProxies(proxies *CredentialProxies, clientFor func(*CredentialProxies) (*http.Client, error), resetBody func() error, do func(*http.Client) (*http.Response, error)) (*http.Response, error) {
	for {
		client, err := clientFor(proxies)
		if err != nil {
			return nil, err
		}
		if resetBody != nil {
			if resetErr := resetBody(); resetErr != nil {
				return nil, resetErr
			}
		}
		resp, doErr := do(client)
		// 407 是代理本身拒绝（认证失败），换下一个代理有意义。
		if doErr == nil && resp != nil && resp.StatusCode == http.StatusProxyAuthRequired {
			if proxies.Advance() {
				_ = resp.Body.Close()
				continue
			}
			return resp, nil
		}
		// 传输错误（代理连不上、连接被重置）时顺延；没有下一个代理则收尾。
		if doErr != nil && proxies.Advance() {
			continue
		}
		return resp, doErr
	}
}

// HTTPClientForProxy 渠道级兼容入口：多代理配置时取第一个代理。
// 供费用查询、额度查询等与具体密钥无关的管理操作使用；转发链路请走
// ProxiesForCredential（按密钥序号配对）。
func (s *sChannel) HTTPClientForProxy(proxyURLCipher string) (*http.Client, error) {
	if proxyURLCipher == "" {
		return s.app.HTTP, nil
	}
	proxyURL, err := s.app.Secrets.Decrypt(proxyURLCipher)
	if err != nil {
		return nil, gerror.Wrap(err, "decrypt channel proxy URL")
	}
	lines := splitProxyLines(proxyURL)
	if len(lines) == 0 {
		return s.app.HTTP, nil
	}
	return appservice.NewProxyHTTPClient(s.app.HTTP, lines[0])
}

// RevealProxyURL 解密并返回渠道代理地址明文（多行，每行一个），仅供管理端
// 编辑回显。未配置代理时返回空串；明文只经 HTTPS 响应下发，不落日志、不缓存。
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
// 管理端「测试代理」按钮专用，不绑定渠道、不需要已保存；
// 多行配置由前端拆行后逐个调用。
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
