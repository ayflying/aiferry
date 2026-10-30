package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/cache"
)

// newTestStore 用真实 SQLite 缓存实现做 nonce 存储，验证的就是子节点线上的那套缓存。
func newTestStore(t *testing.T) cache.Cache {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "cache.db"))
	database, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + path + ")"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Logf("close sqlite: %v", closeErr)
		}
	})
	store, err := cache.NewSQLite(context.Background(), database)
	if err != nil {
		t.Fatalf("create sqlite cache: %v", err)
	}
	return store
}

func TestSignerVerifyAcceptsSignedRequest(t *testing.T) {
	signer := NewSigner(strings.Repeat("k", 32), newTestStore(t))
	body := []byte(`{"version":7}`)
	headers, err := signer.Headers("worker-a", http.MethodPost, "/internal/cluster/config", body)
	if err != nil {
		t.Fatalf("sign headers: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/cluster/config?version=6", nil)
	request.Header = headers
	name, err := signer.Verify(context.Background(), request, body)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if name != "worker-a" {
		t.Fatalf("node = %q", name)
	}
}

func TestSignerRejectsTamperedBody(t *testing.T) {
	signer := NewSigner(strings.Repeat("k", 32), newTestStore(t))
	headers, err := signer.Headers("worker-a", http.MethodPost, "/internal/cluster/config", []byte(`{"version":7}`))
	if err != nil {
		t.Fatalf("sign headers: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/cluster/config", nil)
	request.Header = headers
	if _, err = signer.Verify(context.Background(), request, []byte(`{"version":8}`)); err == nil {
		t.Fatal("tampered body must be rejected")
	}
}

func TestSignerRejectsReplayedNonce(t *testing.T) {
	signer := NewSigner(strings.Repeat("k", 32), newTestStore(t))
	body := []byte(`{}`)
	headers, err := signer.Headers("worker-a", http.MethodPost, "/internal/cluster/heartbeat", body)
	if err != nil {
		t.Fatalf("sign headers: %v", err)
	}
	first := httptest.NewRequest(http.MethodPost, "/internal/cluster/heartbeat", nil)
	first.Header = headers
	if _, err = signer.Verify(context.Background(), first, body); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	second := httptest.NewRequest(http.MethodPost, "/internal/cluster/heartbeat", nil)
	second.Header = headers.Clone()
	if _, err = signer.Verify(context.Background(), second, body); err == nil {
		t.Fatal("replayed nonce must be rejected")
	}
}

func TestSignerRejectsExpiredTimestamp(t *testing.T) {
	signer := NewSigner(strings.Repeat("k", 32), newTestStore(t))
	signer.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	body := []byte(`{}`)
	headers, err := signer.Headers("worker-a", http.MethodPost, "/internal/cluster/config", body)
	if err != nil {
		t.Fatalf("sign headers: %v", err)
	}
	signer.now = func() time.Time { return time.Unix(1_700_000_000, 0).Add(10 * time.Minute) }
	request := httptest.NewRequest(http.MethodPost, "/internal/cluster/config", nil)
	request.Header = headers
	if _, err = signer.Verify(context.Background(), request, body); err == nil {
		t.Fatal("expired timestamp must be rejected")
	}
}

func TestSignerRejectsWrongSecret(t *testing.T) {
	signer := NewSigner(strings.Repeat("k", 32), newTestStore(t))
	headers, err := signer.Headers("worker-a", http.MethodPost, "/internal/cluster/config", nil)
	if err != nil {
		t.Fatalf("sign headers: %v", err)
	}
	other := NewSigner(strings.Repeat("z", 32), newTestStore(t))
	request := httptest.NewRequest(http.MethodPost, "/internal/cluster/config", nil)
	request.Header = headers
	if _, err = other.Verify(context.Background(), request, nil); err == nil {
		t.Fatal("request signed with another secret must be rejected")
	}
}

func TestSignerRequiresAllHeaders(t *testing.T) {
	signer := NewSigner(strings.Repeat("k", 32), newTestStore(t))
	request := httptest.NewRequest(http.MethodPost, "/internal/cluster/config", nil)
	if _, err := signer.Verify(context.Background(), request, nil); err == nil {
		t.Fatal("missing signature headers must be rejected")
	}
}
