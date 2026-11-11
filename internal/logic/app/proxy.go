package app

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"golang.org/x/net/proxy"
)

func NewProxyHTTPClient(base *http.Client, rawURL string) (*http.Client, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return base, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" {
		return nil, gerror.New("proxy URL must use http://user:pass@host:port or socks5://user:pass@host:port")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, gerror.New("proxy URL must not contain a path, query or fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport := cloneTransport(base)
		transport.Proxy = http.ProxyURL(parsed)
		return clientWithTransport(base, transport), nil
	case "socks5", "socks5h":
		return newSOCKS5HTTPClient(base, parsed)
	default:
		return nil, gerror.New("proxy URL must use http://user:pass@host:port or socks5://user:pass@host:port")
	}
}

func newSOCKS5HTTPClient(base *http.Client, parsed *url.URL) (*http.Client, error) {
	var auth *proxy.Auth
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		auth = &proxy.Auth{User: parsed.User.Username(), Password: password}
	}
	dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
	if err != nil {
		return nil, gerror.Wrap(err, "create SOCKS5 proxy dialer")
	}
	transport := cloneTransport(base)
	transport.Proxy = nil
	// SOCKS 代理负责目标域名解析；DialContext 同时传播取消/超时，绝不回退直连。
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, gerror.New("SOCKS5 proxy does not support context dialing")
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return contextDialer.DialContext(ctx, network, address)
	}
	// 自定义 TLS 拨号器可能绕过 DialContext，从而绕过 SOCKS 代理，必须清除。
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	return clientWithTransport(base, transport), nil
}

func cloneTransport(base *http.Client) *http.Transport {
	transport, ok := base.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	return transport.Clone()
}

func clientWithTransport(base *http.Client, transport *http.Transport) *http.Client {
	return &http.Client{Transport: transport, CheckRedirect: base.CheckRedirect, Jar: base.Jar, Timeout: base.Timeout}
}
