package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

type App struct {
	MySQLHost              string
	MySQLPort              int
	MySQLDatabase          string
	MySQLUser              string
	MySQLPassword          string
	RedisAddr              string
	RedisPassword          string
	RedisDB                int
	MasterKey              []byte
	WebRoot                string
	MigrationsDir          string
	MaxFailoverAttempts    int
	FailureThreshold       int64
	ChannelCooldownSeconds int
	CasdoorEndpoint        string
	CasdoorClientID        string
	CasdoorClientSecret    string
	CasdoorAllowedGroup    string
	SessionTTL             int
}

func Load() (App, error) {
	var (
		mysqlPort, errPort = strconv.Atoi(env("MYSQL_PORT", "3306"))
		redisDB, errRedis  = strconv.Atoi(env("REDIS_DB", "0"))
		masterKey, errKey  = base64.StdEncoding.DecodeString(os.Getenv("AIFERRY_MASTER_KEY"))
	)
	if errPort != nil || errRedis != nil {
		return App{}, gerror.New("MYSQL_PORT or REDIS_DB is invalid")
	}
	if errKey != nil || len(masterKey) != 32 {
		return App{}, gerror.New("AIFERRY_MASTER_KEY must be a base64-encoded 32-byte key")
	}
	app := App{
		MySQLHost:              env("MYSQL_HOST", "127.0.0.1"),
		MySQLPort:              mysqlPort,
		MySQLDatabase:          env("MYSQL_DATABASE", "aiferry"),
		MySQLUser:              env("MYSQL_USER", "aiferry"),
		MySQLPassword:          os.Getenv("MYSQL_PASSWORD"),
		RedisAddr:              env("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:          os.Getenv("REDIS_PASSWORD"),
		RedisDB:                redisDB,
		MasterKey:              masterKey,
		WebRoot:                env("WEB_ROOT", "./web"),
		MigrationsDir:          env("MIGRATIONS_DIR", "manifest/migrations"),
		MaxFailoverAttempts:    envInt("MAX_FAILOVER_ATTEMPTS", 3),
		FailureThreshold:       int64(envInt("CHANNEL_FAILURE_THRESHOLD", 3)),
		ChannelCooldownSeconds: envInt("CHANNEL_COOLDOWN_SECONDS", 60),
		CasdoorEndpoint:        strings.TrimRight(env("CASDOOR_ENDPOINT", ""), "/"),
		CasdoorClientID:        strings.TrimSpace(os.Getenv("CASDOOR_CLIENT_ID")),
		CasdoorClientSecret:    os.Getenv("CASDOOR_CLIENT_SECRET"),
		CasdoorAllowedGroup:    env("CASDOOR_ALLOWED_GROUP", "AI用户组"),
		SessionTTL:             envInt("SESSION_TTL_HOURS", 12),
	}
	if strings.TrimSpace(app.MySQLPassword) == "" {
		return App{}, gerror.New("MYSQL_PASSWORD is required")
	}
	if app.CasdoorEndpoint == "" || app.CasdoorClientID == "" || app.CasdoorClientSecret == "" {
		return App{}, gerror.New("CASDOOR_ENDPOINT, CASDOOR_CLIENT_ID and CASDOOR_CLIENT_SECRET are required")
	}
	return app, nil
}

func (c App) MySQLDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local", c.MySQLUser, c.MySQLPassword, c.MySQLHost, c.MySQLPort, c.MySQLDatabase)
}

func (c App) GoFrameMySQLLink() string {
	return "mysql:" + c.MySQLDSN()
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
