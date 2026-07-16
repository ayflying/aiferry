package app

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/service/secret"
)

type Service struct {
	Config  config.App
	Redis   *redis.Client
	Secrets *secret.Service
	HTTP    *http.Client
}

func New(ctx context.Context, cfg config.App) (*Service, error) {
	if err := migrate(cfg); err != nil {
		return nil, err
	}
	if err := gdb.AddConfigNode("default", gdb.ConfigNode{Type: "mysql", Link: cfg.GoFrameMySQLLink()}); err != nil {
		return nil, gerror.Wrap(err, "configure GoFrame database")
	}
	secrets, err := secret.New(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err = redisClient.Ping(ctx).Err(); err != nil {
		return nil, gerror.Wrap(err, "connect Redis")
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 180 * time.Second,
	}
	return &Service{
		Config:  cfg,
		Redis:   redisClient,
		Secrets: secrets,
		HTTP:    &http.Client{Transport: transport},
	}, nil
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
