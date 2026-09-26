package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// 产品约定：登录会话有效期写死 30 天（见 internal/logic/auth/session.go 的 sessionTTLDuration），
// 配置层不再读取 SESSION_TTL_HOURS，也不再暴露 SessionTTL 字段；断言在 auth 包内。

func TestLoadUsesBeijingStorageTimezone(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "test-password")
	t.Setenv("AIFERRY_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("CASDOOR_ENDPOINT", "https://casdoor.example.test")
	t.Setenv("CASDOOR_CLIENT_ID", "test-client")
	t.Setenv("CASDOOR_CLIENT_SECRET", "test-secret")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if time.Local.String() != storageTimezone {
		t.Fatalf("time.Local = %q, want %q", time.Local, storageTimezone)
	}
}

func TestMySQLDSNUsesBeijingStorageLocation(t *testing.T) {
	app := App{MySQLHost: "db.example.test", MySQLPort: 3306, MySQLDatabase: "aiferry", MySQLUser: "user", MySQLPassword: "password"}
	dsn := app.MySQLDSN()
	if !strings.Contains(dsn, "loc=Asia%2FShanghai") {
		t.Fatalf("MySQLDSN() = %q, missing Beijing storage location", dsn)
	}
	if strings.Contains(dsn, "time_zone=") {
		t.Fatalf("MySQLDSN() = %q, must not change the database session timezone", dsn)
	}
}
