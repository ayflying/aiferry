package app

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 本地源站始终可达，测试不访问外网。只要有一次意外直连，源站计数就会暴露它。
func enforcementOrigin(t *testing.T, secure bool, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/result", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "origin")
	})
	var server *httptest.Server
	if secure {
		server = httptest.NewTLSServer(handler)
	} else {
		server = httptest.NewServer(handler)
	}
	t.Cleanup(server.Close)
	return server
}

// 同时实现明文正向请求和真实 CONNECT 隧道，CONNECT 成功后由客户端与源站完成 TLS。
func enforcementHTTPProxy(t *testing.T, secure, reject bool, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if reject {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		if r.Method == http.MethodConnect {
			upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
			if err != nil {
				http.Error(w, "fixture dial failed", 502)
				return
			}
			conn, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				upstream.Close()
				return
			}
			_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = buffer.Flush()
			defer conn.Close()
			defer upstream.Close()
			done := make(chan struct{})
			go func() { _, _ = io.Copy(upstream, buffer); _ = upstream.Close(); close(done) }()
			_, _ = io.Copy(conn, upstream)
			_ = conn.Close()
			<-done
			return
		}
		request := r.Clone(r.Context())
		request.RequestURI = ""
		response, err := transport.RoundTrip(request)
		if err != nil {
			http.Error(w, "fixture forwarding failed", 502)
			return
		}
		defer response.Body.Close()
		for k, values := range response.Header {
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
	var server *httptest.Server
	if secure {
		server = httptest.NewTLSServer(handler)
	} else {
		server = httptest.NewServer(handler)
	}
	t.Cleanup(server.Close)
	return server
}

func enforcementBase(t *testing.T) *http.Client {
	t.Helper()
	// 仅信任本测试的自签名 fixture；不影响任何生产 TLS 配置。
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 2 * time.Second}
}

func TestProxyEnforcementHTTPAndCONNECT(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	for _, tlsOrigin := range []bool{false, true} {
		for _, tlsProxy := range []bool{false, true} {
			for _, scenario := range []string{"success", "redirect", "rejected", "disconnected"} {
				t.Run(fmt.Sprintf("originTLS=%v/proxyTLS=%v/%s", tlsOrigin, tlsProxy, scenario), func(t *testing.T) {
					var originHits, proxyHits atomic.Int32
					origin := enforcementOrigin(t, tlsOrigin, &originHits)
					proxy := enforcementHTTPProxy(t, tlsProxy, scenario == "rejected", &proxyHits)
					if scenario == "disconnected" {
						proxy.Close()
					}
					base := enforcementBase(t)
					client, err := NewProxyHTTPClient(base, proxy.URL)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(client.CloseIdleConnections)
					path := "/result"
					if scenario == "redirect" {
						path = "/redirect"
					}
					response, err := client.Get(origin.URL + path)
					if response != nil {
						_, _ = io.Copy(io.Discard, response.Body)
						response.Body.Close()
					}
					if scenario == "rejected" || scenario == "disconnected" {
						if originHits.Load() != 0 {
							t.Fatalf("failed proxy leaked %d direct requests", originHits.Load())
						}
						if err == nil && (response == nil || response.StatusCode != http.StatusProxyAuthRequired) {
							t.Fatalf("proxy failure not surfaced: %v %v", response, err)
						}
						return
					}
					want := int32(1)
					if scenario == "redirect" {
						want = 2
					}
					if err != nil || originHits.Load() != want || proxyHits.Load() == 0 {
						t.Fatalf("proxy bypass: err=%v origin=%d proxy=%d", err, originHits.Load(), proxyHits.Load())
					}
					// HTTPS redirects may reuse one CONNECT tunnel; HTTP redirects must each hit the forward proxy.
					if !tlsOrigin && proxyHits.Load() != want {
						t.Fatalf("redirect bypassed proxy: got %d want %d", proxyHits.Load(), want)
					}
				})
			}
		}
	}
}

type socksRequest struct {
	host        string
	port        uint16
	addressType byte
}

// 最小 SOCKS5 服务：握手后记录目标原文并拒绝/挂起。域名使用 .invalid，若客户端
// 擅自本地解析，请求不会到达这里；无需劫持 DNS 或依赖外部网络。
func enforcementSOCKS(t *testing.T, stall bool) (string, <-chan socksRequest) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan socksRequest, 4)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop); listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				header := make([]byte, 2)
				if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 {
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				if _, err := conn.Write([]byte{5, 0}); err != nil {
					return
				}
				command := make([]byte, 4)
				if _, err := io.ReadFull(conn, command); err != nil {
					return
				}
				var host string
				switch command[3] {
				case 1:
					address := make([]byte, 4)
					if _, err := io.ReadFull(conn, address); err != nil {
						return
					}
					host = net.IP(address).String()
				case 4:
					address := make([]byte, 16)
					if _, err := io.ReadFull(conn, address); err != nil {
						return
					}
					host = net.IP(address).String()
				case 3:
					size := make([]byte, 1)
					if _, err := io.ReadFull(conn, size); err != nil {
						return
					}
					address := make([]byte, int(size[0]))
					if _, err := io.ReadFull(conn, address); err != nil {
						return
					}
					host = string(address)
				default:
					return
				}
				port := make([]byte, 2)
				if _, err := io.ReadFull(conn, port); err != nil {
					return
				}
				requests <- socksRequest{host: host, port: binary.BigEndian.Uint16(port), addressType: command[3]}
				if stall {
					<-stop
					return
				}
				_, _ = conn.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
			}()
		}
	}()
	return "socks5://" + listener.Addr().String(), requests
}

func TestProxyEnforcementSOCKSRejectsWithoutDirectOrTLSBypass(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprint(secure), func(t *testing.T) {
			var originHits, tlsBypass atomic.Int32
			origin := enforcementOrigin(t, secure, &originHits)
			raw, requests := enforcementSOCKS(t, false)
			base := enforcementBase(t)
			base.Transport.(*http.Transport).DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				tlsBypass.Add(1)
				return tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, network, address, &tls.Config{InsecureSkipVerify: true})
			}
			client, err := NewProxyHTTPClient(base, raw)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Get(origin.URL)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || originHits.Load() != 0 || tlsBypass.Load() != 0 {
				t.Fatalf("SOCKS refusal bypassed: err=%v origin=%d tlsDial=%d", err, originHits.Load(), tlsBypass.Load())
			}
			select {
			case <-requests:
			case <-time.After(time.Second):
				t.Fatal("SOCKS proxy was not used")
			}
		})
	}
}

func TestProxyEnforcementSOCKSDisconnectedNeverDialsOrigin(t *testing.T) {
	var hits atomic.Int32
	origin := enforcementOrigin(t, true, &hits)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	client, err := NewProxyHTTPClient(enforcementBase(t), "socks5://"+address)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(origin.URL)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || hits.Load() != 0 {
		t.Fatalf("disconnected SOCKS leaked direct request: err=%v origin=%d", err, hits.Load())
	}
}

func TestProxyEnforcementSOCKSRemoteDNS(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			raw, requests := enforcementSOCKS(t, false)
			client, err := NewProxyHTTPClient(enforcementBase(t), strings.Replace(raw, "socks5://", scheme+"://", 1))
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Get("https://no-local-resolution.proxy-fixture.invalid:443/models")
			if response != nil {
				response.Body.Close()
			}
			if err == nil {
				t.Fatal("SOCKS fixture refusal not surfaced")
			}
			select {
			case request := <-requests:
				if request.host != "no-local-resolution.proxy-fixture.invalid" || request.port != 443 || request.addressType != 3 {
					t.Fatalf("DNS was not delegated: %+v", request)
				}
			case <-time.After(time.Second):
				t.Fatal("domain did not reach SOCKS proxy; possible local DNS attempt")
			}
		})
	}
}

func TestProxyEnforcementSOCKSCancellationAndTimeout(t *testing.T) {
	for _, mode := range []string{"context-cancel", "client-timeout"} {
		t.Run(mode, func(t *testing.T) {
			raw, requests := enforcementSOCKS(t, true)
			client, err := NewProxyHTTPClient(enforcementBase(t), raw)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "client-timeout" {
				client.Timeout = 150 * time.Millisecond
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://timeout.proxy-fixture.invalid/models", nil)
			result := make(chan error, 1)
			go func() {
				response, e := client.Do(req)
				if response != nil {
					response.Body.Close()
				}
				result <- e
			}()
			select {
			case <-requests:
			case <-time.After(time.Second):
				t.Fatal("SOCKS handshake not reached")
			}
			if mode == "context-cancel" {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("hung SOCKS handshake succeeded")
				}
				if mode == "context-cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation not propagated: %v", err)
				}
				if mode == "client-timeout" {
					var timeout net.Error
					if !errors.As(err, &timeout) || !timeout.Timeout() {
						t.Fatalf("timeout not propagated: %v", err)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("SOCKS connection ignored cancellation/timeout")
			}
		})
	}
}
