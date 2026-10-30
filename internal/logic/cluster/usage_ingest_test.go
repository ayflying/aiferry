package cluster

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/shopspring/decimal"
)

// openUsageDatabase 建一个只含回传相关表的最小库。
func openUsageDatabase(t *testing.T) gdb.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "usage.db"))
	database, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + path + ")"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Logf("close sqlite: %v", closeErr)
		}
	})
	ctx := context.Background()
	statements := []string{
		`CREATE TABLE usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			user_id INTEGER NOT NULL DEFAULT 0,
			api_key_id INTEGER NOT NULL DEFAULT 0,
			estimated_cost REAL NOT NULL DEFAULT 0,
			billing_details_json TEXT,
			node_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT
		)`,
		`CREATE UNIQUE INDEX uk_usage_logs_request ON usage_logs (request_id)`,
		`CREATE TABLE cluster_usage_batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id INTEGER NOT NULL DEFAULT 0,
			batch_id TEXT NOT NULL DEFAULT '',
			rows INTEGER NOT NULL DEFAULT 0,
			created_at TEXT
		)`,
		`CREATE UNIQUE INDEX uk_cluster_usage_batches ON cluster_usage_batches (node_id, batch_id)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(ctx, statement); err != nil {
			t.Fatalf("apply schema: %v\n%s", err, statement)
		}
	}
	return database
}

// fixedBilling 记录扣费调用，便于断言幂等性。
type fixedBilling struct {
	debits []uint64
	spends []float64
}

func (b *fixedBilling) Debit(ctx context.Context, userID uint64, amount decimal.Decimal) error {
	b.debits = append(b.debits, userID)
	return nil
}

func (b *fixedBilling) AddSpend(ctx context.Context, apiKeyID uint64, amount float64) error {
	b.spends = append(b.spends, amount)
	return nil
}

func chargedRow() map[string]any {
	return map[string]any{
		"request_id":           "req-1",
		"user_id":              uint64(7),
		"api_key_id":           uint64(21),
		"estimated_cost":       0.5,
		"billing_details_json": `{"charged":true}`,
	}
}

func TestHandleUsageBillsOncePerBatch(t *testing.T) {
	billing := &fixedBilling{}
	service := &Service{database: openUsageDatabase(t), billing: billing}
	node := &Node{Id: 1, Name: "worker-a"}
	ctx := context.Background()

	ack, err := service.HandleUsage(ctx, node, usageBatch{BatchID: "batch-1", Rows: []map[string]any{chargedRow()}})
	if err != nil {
		t.Fatalf("ingest usage: %v", err)
	}
	if ack.Accepted != 1 || ack.Replayed {
		t.Fatalf("ack = %+v, want 1 accepted", ack)
	}
	if len(billing.debits) != 1 || len(billing.spends) != 1 {
		t.Fatalf("billing = %d debits / %d spends, want 1/1", len(billing.debits), len(billing.spends))
	}

	// 同一批次重放：直接回执成功，不再入账也不再扣费。
	ack, err = service.HandleUsage(ctx, node, usageBatch{BatchID: "batch-1", Rows: []map[string]any{chargedRow()}})
	if err != nil {
		t.Fatalf("replay batch: %v", err)
	}
	if !ack.Replayed || ack.Accepted != 1 {
		t.Fatalf("replay ack = %+v", ack)
	}
	if len(billing.debits) != 1 {
		t.Fatalf("replay debited again: %d", len(billing.debits))
	}

	// 新批次携带同一 request_id：视为重复行，不再扣费。
	ack, err = service.HandleUsage(ctx, node, usageBatch{BatchID: "batch-2", Rows: []map[string]any{chargedRow()}})
	if err != nil {
		t.Fatalf("duplicate row batch: %v", err)
	}
	if ack.Accepted != 0 {
		t.Fatalf("duplicate row accepted = %d, want 0", ack.Accepted)
	}
	if len(billing.debits) != 1 {
		t.Fatalf("duplicate row debited again: %d", len(billing.debits))
	}
	rows, err := service.database.GetCount(ctx, "SELECT COUNT(*) FROM usage_logs")
	if err != nil {
		t.Fatalf("count usage rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("usage rows = %d, want 1", rows)
	}
}

func TestHandleUsageSkipsUnchargedRows(t *testing.T) {
	billing := &fixedBilling{}
	service := &Service{database: openUsageDatabase(t), billing: billing}
	row := chargedRow()
	row["request_id"] = "req-2"
	// 自有渠道：只统计不扣费。
	row["billing_details_json"] = `{"charged":false}`
	ack, err := service.HandleUsage(context.Background(), &Node{Id: 1}, usageBatch{BatchID: "batch-3", Rows: []map[string]any{row}})
	if err != nil {
		t.Fatalf("ingest usage: %v", err)
	}
	if ack.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", ack.Accepted)
	}
	if len(billing.debits) != 0 || len(billing.spends) != 0 {
		t.Fatalf("uncharged row was billed: %d/%d", len(billing.debits), len(billing.spends))
	}
}

func TestHandleUsageRejectsBatchWithoutNode(t *testing.T) {
	service := &Service{database: openUsageDatabase(t)}
	if _, err := service.HandleUsage(context.Background(), nil, usageBatch{BatchID: "b"}); err == nil {
		t.Fatal("batch without node identity must be rejected")
	}
}
