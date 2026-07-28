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

func NewSOCKS5HTTPClient(base *http.Client, rawURL string) (*http.Client, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return base, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "socks5" && parsed.Scheme != "socks5h") || parsed.Host == "" || parsed.Port() == "" {
		return nil, gerror.New("proxy URL must use socks5://user:pass@host:port")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, gerror.New("proxy URL must not contain a query or fragment")
	}
	var auth *proxy.Auth
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		auth = &proxy.Auth{User: parsed.User.Username(), Password: password}
	}
	dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
	if err != nil {
		return nil, gerror.Wrap(err, "create SOCKS5 proxy dialer")
	}
	transport, ok := base.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	cloned := transport.Clone()
	cloned.Proxy = nil
	cloned.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
		return dialer.Dial(network, address)
	}
	return &http.Client{Transport: cloned, CheckRedirect: base.CheckRedirect, Jar: base.Jar, Timeout: base.Timeout}, nil
}
