package cache_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"

	"github.com/yunloli/aiferry/internal/logic/cache"
)

// openTestCache 在临时目录里建一个真实的 SQLite 缓存，用于验证子节点实现。
func openTestCache(t *testing.T) cache.Cache {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker.db")
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + filepath.ToSlash(path) + ")"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Windows 上未关闭的连接会占住文件，导致 TempDir 清理失败。
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	store, err := cache.NewSQLite(context.Background(), db)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	return store
}

func TestSQLiteCacheSetGet(t *testing.T) {
	ctx := context.Background()
	store := openTestCache(t)

	if err := store.Set(ctx, "k1", []byte("v1"), time.Minute).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	value, err := store.Get(ctx, "k1").Bytes()
	if err != nil || string(value) != "v1" {
		t.Fatalf("get = %q, %v", value, err)
	}
	if _, err = store.Get(ctx, "missing").Result(); err == nil {
		t.Fatal("missing key must return error")
	}
}

func TestSQLiteCacheSetNX(t *testing.T) {
	ctx := context.Background()
	store := openTestCache(t)

	created, err := store.SetNX(ctx, "lock", "1", time.Minute).Result()
	if err != nil || !created {
		t.Fatalf("first SetNX = %v, %v", created, err)
	}
	created, err = store.SetNX(ctx, "lock", "1", time.Minute).Result()
	if err != nil || created {
		t.Fatalf("second SetNX = %v, %v", created, err)
	}
}

func TestSQLiteCacheIncrExistsDelTTL(t *testing.T) {
	ctx := context.Background()
	store := openTestCache(t)

	first, err := store.Incr(ctx, "counter").Result()
	if err != nil || first != 1 {
		t.Fatalf("first Incr = %d, %v", first, err)
	}
	second, err := store.Incr(ctx, "counter").Result()
	if err != nil || second != 2 {
		t.Fatalf("second Incr = %d, %v", second, err)
	}

	if err = store.Set(ctx, "entry", "v", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if ttl, err := store.TTL(ctx, "entry").Result(); err != nil || ttl != -1*time.Second {
		t.Fatalf("TTL without expiry = %v, %v", ttl, err)
	}
	if err = store.Expire(ctx, "entry", time.Hour).Err(); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if ttl, err := store.TTL(ctx, "entry").Result(); err != nil || ttl <= 0 || ttl > time.Hour {
		t.Fatalf("TTL after expire = %v, %v", ttl, err)
	}
	if exists, err := store.Exists(ctx, "entry").Result(); err != nil || exists != 1 {
		t.Fatalf("exists = %d, %v", exists, err)
	}
	if deleted, err := store.Del(ctx, "entry", "counter").Result(); err != nil || deleted != 2 {
		t.Fatalf("del = %d, %v", deleted, err)
	}
	if exists, err := store.Exists(ctx, "entry").Result(); err != nil || exists != 0 {
		t.Fatalf("exists after del = %d, %v", exists, err)
	}
}

func TestSQLiteCacheExpiredEntryIsInvisible(t *testing.T) {
	ctx := context.Background()
	store := openTestCache(t)

	// ttl 小于 1 秒时绝对过期时间落在当前秒，按惰性过期语义立即不可见。
	if err := store.Set(ctx, "gone", "v", time.Millisecond).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := store.Get(ctx, "gone").Result(); err == nil {
		t.Fatal("expired entry must not be readable")
	}
	if exists, _ := store.Exists(ctx, "gone").Result(); exists != 0 {
		t.Fatalf("expired entry must not exist, got %d", exists)
	}
	if err := store.Purge(ctx); err != nil {
		t.Fatalf("purge: %v", err)
	}
}

func TestSQLiteCacheGetDel(t *testing.T) {
	ctx := context.Background()
	store := openTestCache(t)

	if err := store.Set(ctx, "once", "v", time.Minute).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	value, err := store.GetDel(ctx, "once").Result()
	if err != nil || value != "v" {
		t.Fatalf("getdel = %q, %v", value, err)
	}
	if _, err = store.GetDel(ctx, "once").Result(); err == nil {
		t.Fatal("second GetDel must report miss")
	}
}
