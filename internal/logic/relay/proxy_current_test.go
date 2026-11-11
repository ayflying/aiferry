package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/yunloli/aiferry/internal/dao"
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
