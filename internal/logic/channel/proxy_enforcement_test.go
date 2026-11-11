package channel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// 使用真实本机 HTTP 正向代理而非 RoundTripper stub；origin 只接受代理注入的标记。
func TestProxyEnforcementManagementRequests(t *testing.T) {
	for _, operation := range []string{"models", "balance", "login"} {
		for _, scenario := range []string{"success", "redirect", "proxy-rejected", "proxy-disconnected"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				var (
					originHits atomic.Int32
					proxyHits  atomic.Int32
				)
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					originHits.Add(1)
					if r.Header.Get("X-Fixture-Proxy") != "enforced" {
						t.Error("origin received a direct request without proxy marker")
					}
					if r.URL.Path == "/redirect" {
						http.Redirect(w, r, "/result", http.StatusFound)
						return
					}
					_, _ = io.WriteString(w, `{"data":[],"balance":42}`)
				}))
				t.Cleanup(origin.Close)
				proxy := newEnforcementProxy(t, &proxyHits, scenario == "proxy-rejected")
				svc, secrets := newUpstreamAuthTestService(t, `{}`)
				svc.app.HTTP = &http.Client{Timeout: 2 * time.Second}
				cipher, err := secrets.Encrypt(proxy.URL)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "proxy-disconnected" {
					proxy.Close()
				}
				endpoint := origin.URL + "/result"
				if scenario == "redirect" {
					endpoint = origin.URL + "/redirect"
				}
				channel := entity.Channels{ProxyUrlCipher: cipher}
				if operation == "login" {
					_, err = svc.fetchLoginJSON(context.Background(), channel, http.MethodGet, endpoint, "")
				} else {
					// 模型发现与余额查询共用的真实传输入口。
					_, err = svc.fetchUpstreamJSON(context.Background(), channel, "", upstreamJSONRequest{
						Method: http.MethodGet, Endpoint: endpoint, AuthType: "none", BodyLimit: 1 << 20,
					})
				}
				if strings.HasPrefix(scenario, "proxy-") {
					if err == nil || originHits.Load() != 0 {
						t.Fatalf("proxy failure must not fall back: err=%v origin=%d", err, originHits.Load())
					}
					return
				}
				want := int32(1)
				if scenario == "redirect" {
					want = 2
				}
				if err != nil || proxyHits.Load() != want || originHits.Load() != want {
					t.Fatalf("err=%v proxy=%d origin=%d want=%d", err, proxyHits.Load(), originHits.Load(), want)
				}
			})
		}
	}
}

func newEnforcementProxy(t *testing.T, hits *atomic.Int32, reject bool) *httptest.Server {
	t.Helper()
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if reject {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		request := r.Clone(r.Context())
		request.RequestURI = ""
		request.Header.Set("X-Fixture-Proxy", "enforced")
		response, err := transport.RoundTrip(request)
		if err != nil {
			http.Error(w, "fixture proxy forwarding failed", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProxyEnforcementClientBuilders(t *testing.T) {
	for _, builder := range []string{"channel", "credential", "plan"} {
		t.Run(builder, func(t *testing.T) {
			var proxyHits, originHits atomic.Int32
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originHits.Add(1)
				if r.Header.Get("X-Fixture-Proxy") != "enforced" {
					t.Error("builder allowed direct origin access")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(origin.Close)
			proxy := newEnforcementProxy(t, &proxyHits, false)
			svc, secrets := newUpstreamAuthTestService(t, `{}`)
			svc.app.HTTP = &http.Client{Timeout: 2 * time.Second}
			cipher, err := secrets.Encrypt(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			var client *http.Client
			switch builder {
			case "channel":
				client, err = svc.HTTPClientForProxy(cipher)
			case "credential":
				// 单代理不查询固定序号，不依赖数据库。
				client, err = svc.HTTPClientForCredential(context.Background(), cipher, 0, 456)
			case "plan":
				client, err = svc.ClientFor(newCredentialProxies([]string{proxy.URL}, 1))
			}
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Get(origin.URL)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if originHits.Load() != 1 || proxyHits.Load() != 1 {
				t.Fatalf("origin=%d proxy=%d", originHits.Load(), proxyHits.Load())
			}
			proxy.Close()
			response, err = client.Get(origin.URL)
			if response != nil {
				_ = response.Body.Close()
			}
			if err == nil || originHits.Load() != 1 {
				t.Fatalf("failed proxy fell back: err=%v origin=%d", err, originHits.Load())
			}
		})
	}
}

func TestProxyEnforcementRejectsConfiguredInvalidCipher(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	svc.app.HTTP = &http.Client{Timeout: time.Second}
	for _, plain := range []string{"", " \n\t\n", "not-a-proxy", "http://127.0.0.1:1234\ninvalid-second-line"} {
		cipher, err := secrets.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(fmt.Sprintf("plain=%q", plain), func(t *testing.T) {
			assertRejectedProxyCipher(t, svc, cipher)
		})
	}
	t.Run("corrupt-cipher", func(t *testing.T) {
		assertRejectedProxyCipher(t, svc, "broken-encrypted-proxy")
	})
	if _, err := svc.ClientFor(newCredentialProxies([]string{" \n "}, 1)); err == nil {
		t.Error("configured blank proxy plan must not produce direct client")
	}
}

func assertRejectedProxyCipher(t *testing.T, svc *sChannel, cipher string) {
	t.Helper()
	if client, err := svc.HTTPClientForProxy(cipher); err == nil || client != nil {
		t.Errorf("channel proxy must reject invalid configured cipher: client=%v err=%v", client, err)
	}
	if client, err := svc.HTTPClientForCredential(context.Background(), cipher, 0, 1); err == nil || client != nil {
		t.Errorf("credential proxy must reject invalid configured cipher: client=%v err=%v", client, err)
	}
}

func TestProxyEnforcementCodexAuthConnectFailureNeverDialsOfficialHost(t *testing.T) {
	var proxyHits, directDials atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.Method != http.MethodConnect || r.Host != "auth.openai.com:443" {
			t.Errorf("expected official auth CONNECT, got %s %s", r.Method, r.Host)
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	t.Cleanup(proxy.Close)
	proxyAddress := strings.TrimPrefix(proxy.URL, "http://")
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != proxyAddress {
			directDials.Add(1)
			return nil, fmt.Errorf("fixture forbids external/direct dial")
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, address)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	svc.app.HTTP = &http.Client{Transport: transport, Timeout: 2 * time.Second}
	cipher, err := secrets.Encrypt(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.HTTPClientForProxy(cipher)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	_, err = codexAuthRequest(context.Background(), client, "/oauth/token", "application/json", `{}`, &result)
	if err == nil || proxyHits.Load() != 1 || directDials.Load() != 0 {
		t.Fatalf("auth must fail at proxy without direct fallback: err=%v proxy=%d direct=%d", err, proxyHits.Load(), directDials.Load())
	}
}
