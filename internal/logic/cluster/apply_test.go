package cluster

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// openWorkerDatabase 在临时目录建一个与子节点一致的内置库，schema 直接读生产用的
// schema.sql，保证测试验证的就是线上那套表结构。
func openWorkerDatabase(t *testing.T) gdb.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "worker.db"))
	database, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + path + ")"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Logf("close sqlite: %v", closeErr)
		}
	})
	script, err := os.ReadFile(filepath.Join("..", "app", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	for _, statement := range strings.Split(string(script), ";\n") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err = database.Exec(context.Background(), statement); err != nil {
			t.Fatalf("apply schema statement: %v\n%s", err, statement)
		}
	}
	return database
}

func sampleSnapshot(version int64) *Snapshot {
	now := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	return &Snapshot{
		Version: version,
		Channels: []entity.Channels{
			{Id: 1, Name: "主渠道", Status: 1, CreatedAt: now, UpdatedAt: now},
		},
		ChannelCredentials: []entity.ChannelCredentials{
			{Id: 10, ChannelId: 1, ApiKeyCipher: "cipher", Status: 1, CreatedAt: now, UpdatedAt: now},
		},
		ModelPrices: []entity.ModelPrices{
			{PublicName: "gpt-4o", BillingMode: "token", InputPrice: 1, OutputPrice: 2, CreatedAt: now, UpdatedAt: now},
		},
		Users: []entity.Users{
			{Id: 7, Name: "alice", Balance: 100, Status: 1, CreatedAt: now, UpdatedAt: now},
		},
		ApiKeys: []entity.ApiKeys{
			{Id: 21, UserId: 7, Name: "默认", KeyHash: "hash", KeyPrefix: "sk-", Status: 1, CreatedAt: now, UpdatedAt: now},
		},
	}
}

func countRows(t *testing.T, database gdb.DB, table string) int64 {
	t.Helper()
	value, err := database.GetValue(context.Background(), "SELECT COUNT(*) FROM "+table)
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return value.Int64()
}

func TestApplySnapshotWritesMirrorRows(t *testing.T) {
	database := openWorkerDatabase(t)
	snapshot := sampleSnapshot(3)
	if err := applySnapshot(context.Background(), database, snapshot); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	if rows := countRows(t, database, "channels"); rows != 1 {
		t.Fatalf("channels rows = %d, want 1", rows)
	}
	if rows := countRows(t, database, "model_prices"); rows != 1 {
		t.Fatalf("model_prices rows = %d, want 1", rows)
	}
	name, err := database.GetValue(context.Background(), "SELECT name FROM channels WHERE id = 1")
	if err != nil {
		t.Fatalf("read channel name: %v", err)
	}
	if name.String() != "主渠道" {
		t.Fatalf("channel name = %q", name.String())
	}
}

func TestApplySnapshotPreservesLocalColumns(t *testing.T) {
	database := openWorkerDatabase(t)
	ctx := context.Background()
	model := entity.ChannelModels{Id: 31, ChannelId: 1, PublicName: "gpt-4o", UpstreamName: "gpt-4o", Enabled: 1, CreatedAt: time.Now().Truncate(time.Millisecond)}
	snapshot := sampleSnapshot(3)
	snapshot.ChannelModels = []entity.ChannelModels{model}
	if err := applySnapshot(ctx, database, snapshot); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	// 健康分是节点自己的观测结果，不能被主节点的快照覆盖。
	if _, err := database.Exec(ctx, "UPDATE channel_models SET health_score = 42 WHERE id = 31"); err != nil {
		t.Fatalf("set local health score: %v", err)
	}
	next := sampleSnapshot(4)
	next.ChannelModels = []entity.ChannelModels{model}
	if err := applySnapshot(ctx, database, next); err != nil {
		t.Fatalf("apply second snapshot: %v", err)
	}
	score, err := database.GetValue(ctx, "SELECT health_score FROM channel_models WHERE id = 31")
	if err != nil {
		t.Fatalf("read health score: %v", err)
	}
	if score.Int() != 42 {
		t.Fatalf("local health score was overwritten: %v", score.Int())
	}
}

func TestApplySnapshotOverwritesBalancesFromMaster(t *testing.T) {
	database := openWorkerDatabase(t)
	ctx := context.Background()
	if err := applySnapshot(ctx, database, sampleSnapshot(3)); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	// 余额与累计消耗以主节点为权威值：主节点充值后必须能覆盖子节点本地记账。
	if _, err := database.Exec(ctx, "UPDATE users SET balance = 12.34 WHERE id = 7"); err != nil {
		t.Fatalf("set local balance: %v", err)
	}
	snapshot := sampleSnapshot(4)
	snapshot.Users[0].Balance = 100
	if err := applySnapshot(ctx, database, snapshot); err != nil {
		t.Fatalf("apply second snapshot: %v", err)
	}
	balance, err := database.GetValue(ctx, "SELECT balance FROM users WHERE id = 7")
	if err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if balance.Float64() != 100 {
		t.Fatalf("balance = %v, want master value 100", balance.Float64())
	}
}

func TestApplySnapshotPrunesRowsRemovedOnMaster(t *testing.T) {
	database := openWorkerDatabase(t)
	ctx := context.Background()
	if err := applySnapshot(ctx, database, sampleSnapshot(3)); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	next := sampleSnapshot(4)
	next.Channels = nil
	next.ApiKeys = nil
	if err := applySnapshot(ctx, database, next); err != nil {
		t.Fatalf("apply pruned snapshot: %v", err)
	}
	if rows := countRows(t, database, "channels"); rows != 0 {
		t.Fatalf("channels rows = %d after prune, want 0", rows)
	}
	if rows := countRows(t, database, "api_keys"); rows != 0 {
		t.Fatalf("api_keys rows = %d after prune, want 0", rows)
	}
	// 渠道被删后其凭据也应从镜像消失，避免残留凭据被路由命中。
	if rows := countRows(t, database, "channel_credentials"); rows != 1 {
		t.Fatalf("channel_credentials rows = %d, want 1 (pruned by snapshot input)", rows)
	}
}

func TestApplySnapshotStoresZeroTimeAsNull(t *testing.T) {
	database := openWorkerDatabase(t)
	ctx := context.Background()
	if err := applySnapshot(ctx, database, sampleSnapshot(3)); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	// deleted_at 为 NULL 时 GoFrame 才认为行未被软删除；写成 0001-01-01 会让整张表
	// 在 DAO 层全部"已删除"。
	deleted, err := database.GetValue(ctx, "SELECT COUNT(*) FROM channels WHERE deleted_at IS NULL")
	if err != nil {
		t.Fatalf("count null deleted_at: %v", err)
	}
	if deleted.Int64() != 1 {
		t.Fatalf("channels with NULL deleted_at = %d, want 1", deleted.Int64())
	}
}
