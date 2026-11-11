package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

func proxyTestRelay(t *testing.T, direct *http.Client) *sRelay {
	t.Helper()
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	base := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	t.Cleanup(base.CloseIdleConnections)
	appSvc := &app.Service{Secrets: secrets, HTTP: base, HTTPDirect: direct}
	return &sRelay{app: appSvc, channels: channel.New(appSvc, channeltype.New(&config.BuiltinRegistry{}, nil), nil, nil, nil, nil, nil, nil)}
}

func TestDirectHTTPHonorsConfiguredProxy(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directCalls.Add(1); _, _ = w.Write([]byte("direct")) }))
	defer upstream.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if !r.URL.IsAbs() {
			t.Error("expected real HTTP proxy request")
		}
		_, _ = w.Write([]byte("proxied"))
	}))
	defer proxy.Close()
	service := proxyTestRelay(t, upstream.Client())
	cipher, err := service.app.Secrets.Encrypt(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, direct := range []bool{false, true} {
		req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/file", nil)
		resp, err := service.doViaProxy(context.Background(), req, Candidate{ProxyURLCipher: cipher, DirectHTTP: direct}, func(client *http.Client) (*http.Response, error) { return client.Do(req) })
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "proxied" {
			t.Fatalf("body=%s", body)
		}
	}
	if proxyCalls.Load() != 2 || directCalls.Load() != 0 {
		t.Fatalf("proxy=%d direct=%d", proxyCalls.Load(), directCalls.Load())
	}
}

func TestUnreachableProxyNeverFallsBackToDirectHTTP(t *testing.T) {
	var directCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directCalls.Add(1) }))
	defer upstream.Close()
	deadProxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadProxy.Close()
	service := proxyTestRelay(t, upstream.Client())
	cipher, err := service.app.Secrets.Encrypt(deadProxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, direct := range []bool{false, true} {
		req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
		resp, err := service.doViaProxy(context.Background(), req, Candidate{ProxyURLCipher: cipher, DirectHTTP: direct}, func(client *http.Client) (*http.Response, error) { return client.Do(req) })
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatal("unreachable proxy unexpectedly succeeded")
		}
	}
	if directCalls.Load() != 0 {
		t.Fatalf("proxy failure leaked %d direct requests", directCalls.Load())
	}
}

func TestNoProxyKeepsDirectHTTPClientSelection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("direct")) }))
	defer upstream.Close()
	service := proxyTestRelay(t, upstream.Client())
	for _, direct := range []bool{false, true} {
		req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
		resp, err := service.doViaProxy(context.Background(), req, Candidate{DirectHTTP: direct}, func(client *http.Client) (*http.Response, error) {
			want := service.app.HTTP
			if direct {
				want = service.app.HTTPDirect
			}
			if client != want {
				t.Error("unconfigured channel changed client selection")
			}
			return client.Do(req)
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
}

func TestVideoTaskRoutePreservesProxyAndCrossOriginDownload(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directCalls.Add(1) }))
	defer cdn.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("OpenAI-Organization") != "" || r.Header.Get("OpenAI-Project") != "" {
			t.Error("cross-origin download leaked upstream credentials")
		}
		_, _ = w.Write([]byte("video"))
	}))
	defer proxy.Close()
	service := proxyTestRelay(t, cdn.Client())
	cipher, err := service.app.Secrets.Encrypt(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	keyCipher, err := service.app.Secrets.Encrypt("synthetic-test-key")
	if err != nil {
		t.Fatal(err)
	}
	original := videoTaskRoute{Candidate: Candidate{BaseURL: "https://upstream.invalid", ProxyURLCipher: cipher, APIKeyCipher: keyCipher, OrganizationID: "org", ProjectID: "project", ChannelCredentialID: 17}, ResourceURL: cdn.URL + "/result.mp4"}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored videoTaskRoute
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Candidate.ProxyURLCipher != cipher || restored.Candidate.ChannelCredentialID != 17 {
		t.Fatal("task route serialization lost proxy identity")
	}
	status, body, _, err := service.downloadAdapterVideo(context.Background(), nil, "task", restored.Candidate, videoAdapter{}, restored.ResourceURL)
	if err != nil || status != http.StatusOK || string(body) != "video" {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	if proxyCalls.Load() != 1 || directCalls.Load() != 0 {
		t.Fatalf("proxy=%d direct=%d", proxyCalls.Load(), directCalls.Load())
	}
	if restored.Candidate.APIKeyCipher != keyCipher || restored.Candidate.ProxyURLCipher != cipher {
		t.Fatal("download mutated persisted candidate")
	}
}
