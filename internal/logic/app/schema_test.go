package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
)

// TestWorkerSchemaCreatesEveryTable 验证内置建表脚本能在真实 SQLite 上完整执行，
// 且重复执行不报错（子节点每次启动都会跑一遍）。
func TestWorkerSchemaCreatesEveryTable(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "schema.db"))
	database, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: sqliteLink(path)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Logf("close sqlite: %v", closeErr)
		}
	})
	ctx := context.Background()
	if err = execSchema(ctx, database); err != nil {
		t.Fatalf("first execSchema: %v", err)
	}
	if err = execSchema(ctx, database); err != nil {
		t.Fatalf("second execSchema must be idempotent: %v", err)
	}
	expected := []string{
		"channels", "channel_credentials", "channel_models", "channel_model_credentials",
		"channel_groups", "channel_group_members", "channel_types", "model_prices",
		"model_price_rules", "price_sources", "system_settings", "users", "api_keys",
		"api_key_models", "api_key_channel_groups", "api_key_channel_credentials",
		"usage_logs", "local_channel_state", "local_credential_state", "local_combo_state",
		"local_events", "sync_state",
	}
	for _, table := range expected {
		value, err := database.GetValue(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if value.Int64() != 1 {
			t.Fatalf("mirror table %s was not created", table)
		}
	}
}

// TestSchemaStatementsDropsComments 防回归：注释里带分号、或建表语句前带注释时，
// 不能把语句本身丢掉。
func TestSchemaStatementsDropsComments(t *testing.T) {
	script := "-- 说明；这里含有全角分号\nCREATE TABLE a (id INTEGER);\n-- 行注释\nCREATE TABLE b (id INTEGER);\n"
	statements := schemaStatements(script)
	if len(statements) != 2 {
		t.Fatalf("statements = %d, want 2: %#v", len(statements), statements)
	}
	if !strings.HasPrefix(statements[0], "CREATE TABLE a") {
		t.Fatalf("first statement = %q", statements[0])
	}
	if !strings.HasPrefix(statements[1], "CREATE TABLE b") {
		t.Fatalf("second statement = %q", statements[1])
	}
}
