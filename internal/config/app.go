package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	// usage_logs uses MySQL DATETIME values that have always represented Beijing wall time.
	// This storage interpretation is fixed; it is independent from the configurable display timezone.
	storageTimezone = "Asia/Shanghai"
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
	ChannelCooldownSeconds int
	CasdoorEndpoint        string
	CasdoorClientID        string
	CasdoorClientSecret    string
	AdminRoles             []string
	// UsageRetentionDays 是使用明细的保留窗口（天）。0 表示不清理（默认）：
	// 仪表盘、用户用量等消费统计都实时聚合自 usage_logs，删除明细会一并缩短
	// 历史统计的可查询窗口，因此只能由部署方显式开启。
	UsageRetentionDays int
	// PayloadLogEnabled 控制「收发原始报文」落盘（默认开启）。每条请求一个 JSON 文件：
	// 客户端请求体 + 聚合后的响应内容，用于事后排查正文污染、参数改写等问题；
	// 文件只含 body，不含任何鉴权头。
	PayloadLogEnabled bool
	// PayloadLogDir 是报文文件的存储目录，容器内必须位于持久化挂载卷下
	//（默认 /app/data/relay-payloads，落在 aiferry_data 卷内），否则容器重建即丢。
	PayloadLogDir string
	// PayloadLogMaxFiles 是目录内最多保留的报文文件数。清理任务每小时按文件修改时间
	// 从旧到新删除超出部分（默认 9999）。
	PayloadLogMaxFiles int
}

func Load() (App, error) {
	if err := setStorageTimezone(); err != nil {
		return App{}, err
	}
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
		ChannelCooldownSeconds: envInt("CHANNEL_COOLDOWN_SECONDS", 60),
		CasdoorEndpoint:        strings.TrimRight(env("CASDOOR_ENDPOINT", ""), "/"),
		CasdoorClientID:        strings.TrimSpace(os.Getenv("CASDOOR_CLIENT_ID")),
		CasdoorClientSecret:    os.Getenv("CASDOOR_CLIENT_SECRET"),
		AdminRoles:             envList("AIFERRY_ADMIN_ROLES", []string{"admin"}),
		UsageRetentionDays:     envInt("USAGE_RETENTION_DAYS", 0),
		PayloadLogEnabled:      envBool("PAYLOAD_LOG_ENABLED", true),
		PayloadLogDir:          env("PAYLOAD_LOG_DIR", "/app/data/relay-payloads"),
		PayloadLogMaxFiles:     envInt("PAYLOAD_LOG_MAX_FILES", 9999),
	}
	if strings.TrimSpace(app.MySQLPassword) == "" {
		return App{}, gerror.New("MYSQL_PASSWORD is required")
	}
	if app.CasdoorEndpoint == "" || app.CasdoorClientID == "" || app.CasdoorClientSecret == "" {
		return App{}, gerror.New("CASDOOR_ENDPOINT, CASDOOR_CLIENT_ID and CASDOOR_CLIENT_SECRET are required")
	}
	return app, nil
}

func setStorageTimezone() error {
	location, err := time.LoadLocation(storageTimezone)
	if err != nil {
		return gerror.Wrap(err, "load storage timezone")
	}
	time.Local = location
	return nil
}

// IsAdminRole centralizes the role-to-permission mapping used by all console APIs.
func (c App) IsAdminRole(role string) bool {
	role = strings.TrimSpace(role)
	for _, configured := range c.AdminRoles {
		if role == configured {
			return true
		}
	}
	return false
}

func (c App) MySQLDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Asia%%2FShanghai", c.MySQLUser, c.MySQLPassword, c.MySQLHost, c.MySQLPort, c.MySQLDatabase)
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

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	if len(result) == 0 {
		return append([]string(nil), fallback...)
	}
	return result
}
