package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// 本测试修改 default 数据库配置，与凭据持久化夹具一致，禁止 parallel。
func TestProxyPolicyUsesCurrentDatabaseConfiguration(t *testing.T) {
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "proxy-policy.db"))
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
		t.Fatal(err)
	}
	db := g.DB("default")
	t.Cleanup(func() { _ = db.Close(ctx) })
	schema, err := os.ReadFile(filepath.Join("..", "app", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err = db.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = db.Exec(ctx, "INSERT INTO channels(id,name) VALUES(901,'proxy policy fixture')"); err != nil {
		t.Fatal(err)
	}
	var originHits, currentProxyHits, oldProxyHits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originHits.Add(1)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(origin.Close)
	currentProxy := newEnforcementProxy(t, &currentProxyHits, false)
	oldProxy := newEnforcementProxy(t, &oldProxyHits, false)
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	svc.app.HTTP = &http.Client{Timeout: 2 * time.Second}
	currentCipher, err := secrets.Encrypt(currentProxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	oldCipher, err := secrets.Encrypt(oldProxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	setProxy := func(cipher string) {
		t.Helper()
		if _, updateErr := db.Exec(ctx, "UPDATE channels SET proxy_url_cipher=? WHERE id=901", cipher); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	call := func(client *http.Client) {
		t.Helper()
		response, callErr := client.Get(origin.URL)
		if callErr != nil {
			t.Fatal(callErr)
		}
		_ = response.Body.Close()
	}
	assertRefused := func(channel entity.Channels) {
		t.Helper()
		beforeOrigin, beforeCurrent, beforeOld := originHits.Load(), currentProxyHits.Load(), oldProxyHits.Load()
		client, policyErr := svc.HTTPClientForChannel(ctx, channel)
		if policyErr == nil || client != nil {
			t.Fatalf("invalid current policy must not return usable client: client=%v err=%v", client, policyErr)
		}
		if originHits.Load() != beforeOrigin || currentProxyHits.Load() != beforeCurrent || oldProxyHits.Load() != beforeOld {
			t.Fatal("policy refusal must perform zero network operations")
		}
	}

	t.Run("new proxy overrides old empty and old proxy snapshots", func(t *testing.T) {
		setProxy(currentCipher)
		got, readErr := svc.CurrentProxyCipher(ctx, 901)
		if readErr != nil || got != currentCipher {
			t.Fatalf("current cipher=%q err=%v", got, readErr)
		}
		for _, stale := range []string{"", oldCipher} {
			client, buildErr := svc.HTTPClientForChannel(ctx, entity.Channels{Id: 901, ProxyUrlCipher: stale})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			call(client)
		}
		if originHits.Load() != 2 || currentProxyHits.Load() != 2 || oldProxyHits.Load() != 0 {
			t.Fatalf("origin=%d current=%d old=%d", originHits.Load(), currentProxyHits.Load(), oldProxyHits.Load())
		}
	})

	t.Run("configured whitespace cannot restore direct access", func(t *testing.T) {
		blankCipher, encryptErr := secrets.Encrypt(" \n\t\n")
		if encryptErr != nil {
			t.Fatal(encryptErr)
		}
		setProxy(blankCipher)
		got, readErr := svc.CurrentProxyCipher(ctx, 901)
		if readErr != nil || got != blankCipher {
			t.Fatalf("configured cipher was lost: got=%q err=%v", got, readErr)
		}
		assertRefused(entity.Channels{Id: 901})
	})

	t.Run("explicitly removed proxy overrides stale proxy and permits no-proxy client", func(t *testing.T) {
		setProxy("")
		got, readErr := svc.CurrentProxyCipher(ctx, 901)
		if readErr != nil || got != "" {
			t.Fatalf("explicit removal cipher=%q err=%v", got, readErr)
		}
		client, buildErr := svc.HTTPClientForChannel(ctx, entity.Channels{Id: 901, ProxyUrlCipher: oldCipher})
		if buildErr != nil || client != svc.app.HTTP {
			t.Fatalf("explicit removal must use base no-proxy client: client=%v err=%v", client, buildErr)
		}
		beforeOrigin, beforeProxy := originHits.Load(), currentProxyHits.Load()+oldProxyHits.Load()
		call(client)
		if originHits.Load() != beforeOrigin+1 || currentProxyHits.Load()+oldProxyHits.Load() != beforeProxy {
			t.Fatal("explicit no-proxy policy did not reach origin directly")
		}
	})

	t.Run("nonexistent or deleted channel is refused despite stale usable proxy", func(t *testing.T) {
		if _, readErr := svc.CurrentProxyCipher(ctx, 999999); readErr == nil {
			t.Fatal("missing channel must not be interpreted as no proxy")
		}
		assertRefused(entity.Channels{Id: 999999, ProxyUrlCipher: oldCipher})
		if _, updateErr := db.Exec(ctx, "UPDATE channels SET deleted_at=CURRENT_TIMESTAMP WHERE id=901"); updateErr != nil {
			t.Fatal(updateErr)
		}
		if _, readErr := svc.CurrentProxyCipher(ctx, 901); readErr == nil {
			t.Fatal("soft-deleted channel must not provide a proxy policy")
		}
		assertRefused(entity.Channels{Id: 901, ProxyUrlCipher: oldCipher})
	})

	// 删除查询所需表制造真实 SQL 错误，不能依赖旧 entity 或错误后回退直连。
	t.Run("database query failure is fail-closed with zero network", func(t *testing.T) {
		if _, dropErr := db.Exec(ctx, "DROP TABLE channels"); dropErr != nil {
			t.Fatal(dropErr)
		}
		if _, readErr := svc.CurrentProxyCipher(ctx, 901); readErr == nil {
			t.Fatal("database failure must not be interpreted as no proxy")
		}
		assertRefused(entity.Channels{Id: 901})
		assertRefused(entity.Channels{Id: 901, ProxyUrlCipher: oldCipher})
	})
}
