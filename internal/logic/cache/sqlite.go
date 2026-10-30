package cache

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
)

// sqliteCache 是子节点使用的缓存实现：把缓存存放在内置 SQLite 文件里，
// 因此子节点不需要任何外部 Redis。
type sqliteCache struct {
	db gdb.DB
}

const sqliteCacheSchema = `
CREATE TABLE IF NOT EXISTS cache_entries (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS cache_counters (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL DEFAULT 0
);`

// NewSQLite 基于内置 SQLite 连接构造缓存，并确保缓存表存在。
func NewSQLite(ctx context.Context, db gdb.DB) (Cache, error) {
	if db == nil {
		return nil, gerror.New("cache: sqlite database is nil")
	}
	for _, statement := range strings.Split(sqliteCacheSchema, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			return nil, gerror.Wrap(err, "create cache tables")
		}
	}
	return &sqliteCache{db: db}, nil
}

func (c *sqliteCache) Get(ctx context.Context, key string) String {
	value, err := c.db.GetValue(ctx,
		"SELECT value FROM cache_entries WHERE key = ? AND (expires_at = 0 OR expires_at > ?)", key, nowUnix())
	if err != nil {
		return stringCmd("", err)
	}
	if value.IsNil() {
		return stringCmd("", ErrMiss)
	}
	return stringCmd(value.String(), nil)
}

func (c *sqliteCache) GetDel(ctx context.Context, key string) String {
	var result String
	err := c.db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		bound := tx.Ctx(ctx)
		value, err := bound.GetValue(
			"SELECT value FROM cache_entries WHERE key = ? AND (expires_at = 0 OR expires_at > ?)", key, nowUnix())
		if err != nil {
			return err
		}
		if value.IsNil() {
			result = stringCmd("", ErrMiss)
			return nil
		}
		if _, err = bound.Exec("DELETE FROM cache_entries WHERE key = ?", key); err != nil {
			return err
		}
		result = stringCmd(value.String(), nil)
		return nil
	})
	if err != nil {
		return stringCmd("", err)
	}
	return result
}

func (c *sqliteCache) Set(ctx context.Context, key string, value any, ttl time.Duration) Status {
	_, err := c.db.Exec(ctx,
		`INSERT INTO cache_entries(key, value, expires_at) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at`,
		key, encodeValue(value), expiryFor(ttl))
	return statusCmd(err)
}

func (c *sqliteCache) SetNX(ctx context.Context, key string, value any, ttl time.Duration) Boolean {
	var created bool
	err := c.db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		bound := tx.Ctx(ctx)
		// 已过期的同名条目视为不存在，与 Redis 的惰性过期语义一致。
		if _, err := bound.Exec(
			"DELETE FROM cache_entries WHERE key = ? AND expires_at > 0 AND expires_at <= ?", key, nowUnix()); err != nil {
			return err
		}
		existing, err := bound.GetValue("SELECT COUNT(*) FROM cache_entries WHERE key = ?", key)
		if err != nil {
			return err
		}
		if !existing.IsNil() && existing.Int64() > 0 {
			return nil
		}
		if _, err = bound.Exec("INSERT INTO cache_entries(key, value, expires_at) VALUES(?, ?, ?)",
			key, encodeValue(value), expiryFor(ttl)); err != nil {
			return err
		}
		created = true
		return nil
	})
	return boolCmd(created, err)
}

func (c *sqliteCache) Del(ctx context.Context, keys ...string) Int {
	if len(keys) == 0 {
		return intCmd(0, nil)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	// Redis 是单键空间，计数器与普通条目共用 key 命名空间，
	// 因此删除时两张表都要清理，否则版本号类计数器会残留。
	entries, err := c.db.Exec(ctx, "DELETE FROM cache_entries WHERE key IN ("+placeholders+")", args...)
	if err != nil {
		return intCmd(0, err)
	}
	counters, err := c.db.Exec(ctx, "DELETE FROM cache_counters WHERE key IN ("+placeholders+")", args...)
	if err != nil {
		return intCmd(0, err)
	}
	entryRows, _ := entries.RowsAffected()
	counterRows, _ := counters.RowsAffected()
	return intCmd(entryRows+counterRows, nil)
}

func (c *sqliteCache) Exists(ctx context.Context, key string) Int {
	value, err := c.db.GetValue(ctx,
		"SELECT COUNT(*) FROM cache_entries WHERE key = ? AND (expires_at = 0 OR expires_at > ?)", key, nowUnix())
	if err != nil {
		return intCmd(0, err)
	}
	if value.IsNil() {
		return intCmd(0, nil)
	}
	return intCmd(value.Int64(), nil)
}

func (c *sqliteCache) Incr(ctx context.Context, key string) Int {
	var value int64
	err := c.db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		bound := tx.Ctx(ctx)
		if _, err := bound.Exec(
			"INSERT INTO cache_counters(key, value) VALUES(?, 0) ON CONFLICT(key) DO NOTHING", key); err != nil {
			return err
		}
		if _, err := bound.Exec("UPDATE cache_counters SET value = value + 1 WHERE key = ?", key); err != nil {
			return err
		}
		current, err := bound.GetValue("SELECT value FROM cache_counters WHERE key = ?", key)
		if err != nil {
			return err
		}
		value = current.Int64()
		return nil
	})
	return intCmd(value, err)
}

func (c *sqliteCache) Expire(ctx context.Context, key string, ttl time.Duration) Boolean {
	result, err := c.db.Exec(ctx, "UPDATE cache_entries SET expires_at = ? WHERE key = ?", expiryFor(ttl), key)
	if err != nil {
		return boolCmd(false, err)
	}
	affected, _ := result.RowsAffected()
	return boolCmd(affected > 0, nil)
}

func (c *sqliteCache) TTL(ctx context.Context, key string) Duration {
	value, err := c.db.GetValue(ctx, "SELECT expires_at FROM cache_entries WHERE key = ?", key)
	if err != nil {
		return durationCmd(0, err)
	}
	if value.IsNil() {
		return durationCmd(-2*time.Second, nil)
	}
	expiresAt := value.Int64()
	if expiresAt == 0 {
		return durationCmd(-1*time.Second, nil)
	}
	remaining := expiresAt - nowUnix()
	if remaining <= 0 {
		return durationCmd(-2*time.Second, nil)
	}
	return durationCmd(time.Duration(remaining)*time.Second, nil)
}

// Purge 删除已过期条目，由子节点的维护协程周期性调用。
func (c *sqliteCache) Purge(ctx context.Context) error {
	if _, err := c.db.Exec(ctx, "DELETE FROM cache_entries WHERE expires_at > 0 AND expires_at <= ?", nowUnix()); err != nil {
		return gerror.Wrap(err, "purge expired cache entries")
	}
	return nil
}

func nowUnix() int64 { return time.Now().Unix() }

// expiryFor 把 TTL 换算成绝对过期时间戳；0 或负值表示永不过期。
func expiryFor(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return nowUnix() + int64(ttl/time.Second)
}

// 编译期校验实现覆盖接口。
var _ Cache = (*sqliteCache)(nil)
