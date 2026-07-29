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
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, gerror.New("proxy URL must not contain a query or fragment")
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
	transport.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
		return dialer.Dial(network, address)
	}
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
