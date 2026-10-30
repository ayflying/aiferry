package app

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/cache"
)

// openMaster 装配主节点：执行数据库迁移、接入 MySQL、连接并校验 Redis。
func (s *sApp) openMaster(ctx context.Context) error {
	if err := migrate(s.Config); err != nil {
		return err
	}
	if err := gdb.AddConfigNode("default", gdb.ConfigNode{Type: "mysql", Link: s.Config.GoFrameMySQLLink()}); err != nil {
		return gerror.Wrap(err, "configure GoFrame database")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     s.Config.RedisAddr,
		Password: s.Config.RedisPassword,
		DB:       s.Config.RedisDB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return gerror.Wrap(err, "connect Redis")
	}
	s.Redis = cache.NewRedis(client)
	return nil
}

func migrate(cfg config.App) error {
	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		return gerror.Wrap(err, "open migration database")
	}
	defer db.Close()
	if err = db.Ping(); err != nil {
		return gerror.Wrap(err, "connect migration database")
	}
	goose.SetDialect("mysql")
	if err = goose.Up(db, cfg.MigrationsDir); err != nil {
		return gerror.Wrap(err, "apply database migrations")
	}
	return nil
}