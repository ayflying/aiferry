package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisCache 是主节点使用的缓存实现：把命令式返回值转换为轻量命令对象，
// 使既有调用点无需改动。
type redisCache struct {
	client *redis.Client
}

// NewRedis 包装一个已连接并完成 Ping 的 Redis 客户端。
func NewRedis(client *redis.Client) Cache {
	return &redisCache{client: client}
}

func (c *redisCache) Get(ctx context.Context, key string) String {
	value, err := c.client.Get(ctx, key).Result()
	return stringCmd(value, convertMiss(err))
}

func (c *redisCache) GetDel(ctx context.Context, key string) String {
	value, err := c.client.GetDel(ctx, key).Result()
	return stringCmd(value, convertMiss(err))
}

func (c *redisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) Status {
	return statusCmd(c.client.Set(ctx, key, value, ttl).Err())
}

func (c *redisCache) SetNX(ctx context.Context, key string, value any, ttl time.Duration) Boolean {
	created, err := c.client.SetNX(ctx, key, value, ttl).Result()
	return boolCmd(created, err)
}

func (c *redisCache) Del(ctx context.Context, keys ...string) Int {
	if len(keys) == 0 {
		return intCmd(0, nil)
	}
	deleted, err := c.client.Del(ctx, keys...).Result()
	return intCmd(deleted, err)
}

func (c *redisCache) Exists(ctx context.Context, key string) Int {
	count, err := c.client.Exists(ctx, key).Result()
	return intCmd(count, err)
}

func (c *redisCache) Incr(ctx context.Context, key string) Int {
	value, err := c.client.Incr(ctx, key).Result()
	return intCmd(value, err)
}

func (c *redisCache) Expire(ctx context.Context, key string, ttl time.Duration) Boolean {
	updated, err := c.client.Expire(ctx, key, ttl).Result()
	return boolCmd(updated, err)
}

func (c *redisCache) TTL(ctx context.Context, key string) Duration {
	value, err := c.client.TTL(ctx, key).Result()
	return durationCmd(value, err)
}

// Purge 对 Redis 是空操作：过期由 Redis 自身完成。
func (c *redisCache) Purge(context.Context) error { return nil }

// convertMiss 把 redis.Nil 归一到 ErrMiss，并保留其它错误原样返回。
func convertMiss(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, redis.Nil) {
		return ErrMiss
	}
	return err
}
