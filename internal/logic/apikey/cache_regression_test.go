package apikey

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

func newCacheRegressionService(t *testing.T) (*sAPIKey, gdb.DB, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "authentication.db"))
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
		t.Fatal(err)
	}
	db := g.DB("default")
	t.Cleanup(func() { _ = db.Close(ctx) })
	schema, err := os.ReadFile(filepath.Join("..", "app", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";\n") {
		if strings.TrimSpace(statement) != "" {
			if _, err = db.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	storage, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, hash, err := secret.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO users(id,name,role,status) VALUES(1,'test','user',1)",
		"INSERT INTO api_keys(id,user_id,key_hash,status,spend_limit,daily_spend_date) VALUES(1,1,?,1,10,date('now','localtime'))",
		"INSERT INTO api_key_models(api_key_id,model_name) VALUES(1,'old-model')",
		"INSERT INTO api_key_channel_groups(api_key_id,channel_group_id) VALUES(1,3)",
		"INSERT INTO user_channel_groups(user_id,channel_group_id) VALUES(1,4)",
	} {
		var args []any
		if strings.Contains(statement, "?") {
			args = []any{hash}
		}
		if _, err = db.Exec(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	return New(&app.Service{Redis: storage}), db, plain
}

func TestAuthenticationCacheMissAndSpendByIDInvalidation(t *testing.T) {
	s, _, plain := newCacheRegressionService(t)
	ctx := context.Background()
	key, err := s.Authenticate(ctx, plain)
	if err != nil || key.Id != 1 {
		t.Fatalf("first authentication on cache miss: key=%+v err=%v", key, err)
	}
	if err = s.app.Redis.Get(ctx, cacheKey(key.KeyHash)).Err(); err != nil {
		t.Fatalf("master must cache authentication: %v", err)
	}
	if err = s.AddSpendByID(ctx, key.Id, 10); err != nil {
		t.Fatal(err)
	}
	if err = s.app.Redis.Get(ctx, cacheKey(key.KeyHash)).Err(); !errors.Is(err, cache.ErrMiss) {
		t.Fatalf("spend must immediately invalidate hash cache: %v", err)
	}
	if _, err = s.Authenticate(ctx, plain); err == nil {
		t.Fatal("spent-out key must not authenticate using stale cached amount")
	}
}

func TestSpendByIDUsesCallerTransaction(t *testing.T) {
	s, db, _ := newCacheRegressionService(t)
	ctx := context.Background()
	rollback := errors.New("rollback test")
	err := db.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if spendErr := s.AddSpendByID(txCtx, 1, 2); spendErr != nil {
			return spendErr
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("expected caller rollback: %v", err)
	}
	spent, err := db.GetValue(ctx, "SELECT spent_amount FROM api_keys WHERE id=1")
	if err != nil || spent.Float64() != 0 {
		t.Fatalf("spending escaped caller transaction: amount=%v err=%v", spent, err)
	}
}

func TestConcurrentSQLiteSpendByID(t *testing.T) {
	s, db, _ := newCacheRegressionService(t)
	ctx := context.Background()
	const count = 8
	start := make(chan struct{})
	results := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- s.AddSpendByID(ctx, 1, 0.25)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent SQLite spending: %v", err)
		}
	}
	spent, err := db.GetValue(ctx, "SELECT spent_amount FROM api_keys WHERE id=1")
	if err != nil || spent.Float64() != count*0.25 {
		t.Fatalf("concurrent spending lost updates: amount=%v err=%v", spent, err)
	}
}

func TestWorkerAuthenticationBypassesStaleSnapshotCache(t *testing.T) {
	s, db, plain := newCacheRegressionService(t)
	s.app.Config.Cluster.Role = config.RoleWorker
	ctx := context.Background()
	key, err := s.Authenticate(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.app.Redis.Get(ctx, cacheKey(key.KeyHash)).Err(); !errors.Is(err, cache.ErrMiss) {
		t.Fatalf("worker must not write AuthKey cache: %v", err)
	}
	encoded, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.app.Redis.Set(ctx, cacheKey(key.KeyHash), encoded, cacheTTL).Err(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE api_key_models SET model_name='new-model' WHERE api_key_id=1",
		"UPDATE api_key_channel_groups SET channel_group_id=5 WHERE api_key_id=1",
		"UPDATE users SET role='admin' WHERE id=1",
		"UPDATE user_channel_groups SET channel_group_id=6 WHERE user_id=1",
	} {
		if _, err = db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	key, err = s.Authenticate(ctx, plain)
	if err != nil || !s.CanUseModel(key, "new-model") || s.CanUseModel(key, "old-model") || !s.CanUseChannelGroups(key, []uint64{5}) || key.UserRole != "admin" || !containsID(key.UserChannelGroupIDs, 6) {
		t.Fatalf("worker must load current local policy: key=%+v err=%v", key, err)
	}
	if _, err = db.Exec(ctx, "UPDATE api_keys SET status=0 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, plain); err == nil {
		t.Fatal("worker must reject disabled key despite stale cache")
	}
	if _, err = db.Exec(ctx, "DELETE FROM api_keys WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, plain); err == nil {
		t.Fatal("worker must reject removed key despite stale cache")
	}
}
