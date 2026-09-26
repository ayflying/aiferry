package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// 产品约定：未显式配置 SESSION_TTL_HOURS 时会话默认 30 天，且每次请求滑动延长。
// 之前默认 7 天、生产又把它压到 12 小时，导致隔夜就掉登录。
func TestLoadUsesThirtyDaySessionByDefault(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "test-password")
	t.Setenv("AIFERRY_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("CASDOOR_ENDPOINT", "https://casdoor.example.test")
	t.Setenv("CASDOOR_CLIENT_ID", "test-client")
	t.Setenv("CASDOOR_CLIENT_SECRET", "test-secret")
	t.Setenv("SESSION_TTL_HOURS", "")

	app, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	const thirtyDaysInHours = 24 * 30
	if defaultSessionTTLHours != thirtyDaysInHours {
		t.Fatalf("defaultSessionTTLHours = %d, want %d", defaultSessionTTLHours, thirtyDaysInHours)
	}
	if app.SessionTTL != thirtyDaysInHours {
		t.Fatalf("SessionTTL = %d, want %d", app.SessionTTL, thirtyDaysInHours)
	}
}

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
