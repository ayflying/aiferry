package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/model/do"
)

func currentProxyDB(t *testing.T) gdb.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "proxy.db"))
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
		t.Fatal(err)
	}
	db := g.DB("default")
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	if _, err := db.Exec(context.Background(), `CREATE TABLE channels (id INTEGER PRIMARY KEY, proxy_url_cipher TEXT NOT NULL DEFAULT '', deleted_at DATETIME)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCurrentProxyOverridesStaleTaskAndRouteSnapshot(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	ctx := context.Background()
	currentProxyDB(t)
	if _, err := dao.Channels.Ctx(ctx).Data(do.Channels{Id: 31, ProxyUrlCipher: ""}).Insert(); err != nil {
		t.Fatal(err)
	}
	var directCalls, proxyCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directCalls.Add(1); _, _ = w.Write([]byte("direct")) }))
	defer upstream.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); _, _ = w.Write([]byte("proxy")) }))
	defer proxy.Close()
	service := proxyTestRelay(t, upstream.Client())
	// Represents a previously cached route/video task before a proxy was added.
	stale := Candidate{ChannelID: 31, DirectHTTP: true, ProxyURLCipher: ""}
	cipher, err := service.app.Secrets.Encrypt(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: 31}).Data(do.Channels{ProxyUrlCipher: cipher}).Update(); err != nil {
		t.Fatal(err)
	}
	for _, direct := range []bool{false, true} {
		stale.DirectHTTP = direct
		req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
		resp, err := service.doViaProxy(ctx, req, stale, func(client *http.Client) (*http.Response, error) { return client.Do(req) })
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "proxy" {
			t.Fatalf("stale candidate bypassed current proxy: %s", body)
		}
	}
	if proxyCalls.Load() != 2 || directCalls.Load() != 0 {
		t.Fatalf("proxy=%d direct=%d", proxyCalls.Load(), directCalls.Load())
	}
	if stale.ProxyURLCipher != "" {
		t.Fatal("caller snapshot mutated")
	}
}

func TestAntigravityProxyHeaderFollowsCredentialProxy(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	ctx := context.Background()
	currentProxyDB(t)
	if _, err := dao.Channels.Ctx(ctx).Data(do.Channels{Id: 41, ProxyUrlCipher: ""}).Insert(); err != nil {
		t.Fatal(err)
	}
	// 两个假代理：第一个记录后直接挂断连接（模拟代理层不可达，走传输错误顺延），
	// 第二个记录收到的内部代理头并放行。不用 407 模拟：Go 的传输层会把 407 当作
	// 代理认证挑战在内部处理，测试会卡在认证循环上。
	var mu sync.Mutex
	var seen []string
	record := func(r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get(channel.AntigravityProxyHeader))
		mu.Unlock()
	}
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte("ok"))
	}))
	defer second.Close()
	service := proxyTestRelay(t, first.Client())
	cipher, err := service.app.Secrets.Encrypt(first.URL + "\n" + second.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: 41}).Data(do.Channels{ProxyUrlCipher: cipher}).Update(); err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{ChannelID: 41, ChannelType: "gemini_antigravity", DirectHTTP: true}
	req, _ := http.NewRequest(http.MethodGet, second.URL, nil)
	resp, err := service.doViaProxy(ctx, req, candidate, func(client *http.Client) (*http.Response, error) { return client.Do(req) })
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	mu.Lock()
	// 代理层失败顺延到第二个代理时，头里必须是顺延后的地址，而不是失效的第一个。
	if len(seen) != 2 || seen[0] != first.URL || seen[1] != second.URL {
		mu.Unlock()
		t.Fatalf("proxy header per attempt = %q, want first then second", seen)
	}
	mu.Unlock()
	// 非 Antigravity 渠道不带该头：同一个渠道、同一组代理，只把类型换成 openai，
	// 请求照常经代理送达，但代理不应再收到代理地址（避免泄露给无关上游）。
	// 用真正的上游而不是「代理即上游」，否则单服务器会在自身上死锁。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("up")) }))
	defer upstream.Close()
	candidate.ChannelType = "openai"
	req, _ = http.NewRequest(http.MethodGet, upstream.URL, nil)
	resp, err = service.doViaProxy(ctx, req, candidate, func(client *http.Client) (*http.Response, error) { return client.Do(req) })
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if seen[len(seen)-1] != "" {
		t.Fatalf("non-antigravity channel leaked proxy header %q", seen[len(seen)-1])
	}
}

func TestCurrentProxyReadFailureAndMissingChannelNeverDial(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	ctx := context.Background()
	db := currentProxyDB(t)
	var directCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directCalls.Add(1) }))
	defer upstream.Close()
	service := proxyTestRelay(t, upstream.Client())
	for _, failure := range []string{"missing channel", "database failure"} {
		if failure == "database failure" {
			if _, err := db.Exec(ctx, "DROP TABLE channels"); err != nil {
				t.Fatal(err)
			}
		}
		for _, direct := range []bool{false, true} {
			req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
			called := false
			resp, err := service.doViaProxy(ctx, req, Candidate{ChannelID: 31, DirectHTTP: direct}, func(client *http.Client) (*http.Response, error) { called = true; return client.Do(req) })
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil || called {
				t.Fatalf("%s: failure did not close outbound path: err=%v called=%t", failure, err, called)
			}
		}
	}
	if directCalls.Load() != 0 {
		t.Fatalf("failed policy lookup leaked %d direct calls", directCalls.Load())
	}
}
