// Package cache 定义节点内缓存的统一抽象。
//
// 主节点使用 Redis 实现（跨进程共享），子节点使用内置 SQLite 实现（零外部依赖）。
// 接口刻意保持 go-redis 的命令式返回形状（Get 返回可 .Result()/.Bytes()/.Int64()
// 的命令对象），这样上百处既有调用点无需改动，只替换注入的实现。
package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// ErrMiss 表示 key 不存在。语义对齐 redis.Nil：调用方既有代码一律用
// err != nil 判定未命中，因此这里必须返回错误而不是空值。
var ErrMiss = errors.New("cache: key not found")

// Cache 是节点内缓存的读写接口。
type Cache interface {
	Get(ctx context.Context, key string) String
	GetDel(ctx context.Context, key string) String
	Set(ctx context.Context, key string, value any, ttl time.Duration) Status
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) Boolean
	Del(ctx context.Context, keys ...string) Int
	Exists(ctx context.Context, key string) Int
	Incr(ctx context.Context, key string) Int
	Expire(ctx context.Context, key string, ttl time.Duration) Boolean
	TTL(ctx context.Context, key string) Duration
	// Purge 清理已过期的条目。Redis 实现为空操作（自身负责过期），
	// SQLite 实现由子节点的维护协程周期性调用。
	Purge(ctx context.Context) error
}

// String 是字符串读命令的返回值。
type String struct {
	val string
	err error
}

// Result 返回字符串值。
func (c String) Result() (string, error) { return c.val, c.err }

// Bytes 返回字节切片值。
func (c String) Bytes() ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	return []byte(c.val), nil
}

// Int64 返回整数值，解析失败时返回错误。
func (c String) Int64() (int64, error) {
	if c.err != nil {
		return 0, c.err
	}
	parsed, err := strconv.ParseInt(c.val, 10, 64)
	if err != nil {
		return 0, gerror.Wrap(err, "cache: value is not an integer")
	}
	return parsed, nil
}

// Uint64 返回无符号整数值，解析失败时返回错误。
func (c String) Uint64() (uint64, error) {
	if c.err != nil {
		return 0, c.err
	}
	parsed, err := strconv.ParseUint(c.val, 10, 64)
	if err != nil {
		return 0, gerror.Wrap(err, "cache: value is not an unsigned integer")
	}
	return parsed, nil
}

// Err 只返回错误。
func (c String) Err() error { return c.err }

// Status 是写入类命令的返回值。
type Status struct {
	val string
	err error
}

// Result 返回写入结果（与 go-redis 的 StatusCmd 形状一致）。
func (c Status) Result() (string, error) { return c.val, c.err }

// Err 只返回错误。
func (c Status) Err() error { return c.err }

// Boolean 是布尔类命令的返回值。
type Boolean struct {
	val bool
	err error
}

// Result 返回布尔结果。
func (c Boolean) Result() (bool, error) { return c.val, c.err }

// Err 只返回错误。
func (c Boolean) Err() error { return c.err }

// Int 是整数类命令的返回值（计数、删除条数、存在性）。
type Int struct {
	val int64
	err error
}

// Result 返回整数结果。
func (c Int) Result() (int64, error) { return c.val, c.err }

// Err 只返回错误。
func (c Int) Err() error { return c.err }

// Duration 是 TTL 类命令的返回值。无过期返回 -1，key 不存在返回 -2。
type Duration struct {
	val time.Duration
	err error
}

// Result 返回 TTL 结果。
func (c Duration) Result() (time.Duration, error) { return c.val, c.err }

// Err 只返回错误。
func (c Duration) Err() error { return c.err }

func stringCmd(value string, err error) String { return String{val: value, err: err} }

func statusCmd(err error) Status { return Status{err: err} }

func boolCmd(value bool, err error) Boolean { return Boolean{val: value, err: err} }

func intCmd(value int64, err error) Int { return Int{val: value, err: err} }

func durationCmd(value time.Duration, err error) Duration {
	return Duration{val: value, err: err}
}

// encodeValue 把任意值转换为字符串存储。callers 只会传 string、[]byte 或整数。
func encodeValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
