package relay

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/logic/user"
)

// 每例隔离 GoFrame 进程级 default DAO 实例，不读取应用配置或外部数据库。
func isolatedAtomicUsageTest(t *testing.T) bool {
	t.Helper()
	const marker = "AIFERRY_TEST_ATOMIC_USAGE_CHILD"
	if os.Getenv(marker) == t.Name() {
		adapter, err := gcfg.NewAdapterContent("{}")
		if err != nil {
			t.Fatal(err)
		}
		g.Cfg().SetAdapter(adapter)
		if err = gdb.SetConfig(gdb.Config{}); err != nil {
			t.Fatal(err)
		}
		return false
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated usage accounting: %v\n%s", err, output)
	}
	return true
}

func newAtomicUsageRelay(t *testing.T) (*sRelay, gdb.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "ledger.db"))
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
		t.Fatal(err)
	}
	db := g.DB("default")
	t.Cleanup(func() { _ = db.Close(ctx) })
	schema, err := os.ReadFile(filepath.Join("..", "app", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";\n") {
		if strings.TrimSpace(statement) != "" {
			if _, err = db.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	appSvc := &app.Service{Redis: store, Config: config.App{Cluster: config.Cluster{Role: config.RoleWorker}}}
	for _, statement := range []string{
		"INSERT INTO users(id,name,balance,status) VALUES(1,'alice',10,1)",
		"INSERT INTO api_keys(id,user_id,key_hash,status,daily_spend_date) VALUES(1,1,'hash',1,date('now','localtime'))",
		"INSERT INTO model_prices(public_name,billing_mode,request_price) VALUES('paid','request',2)",
	} {
		if _, err = db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	prices := pricingcache.New()
	if err = prices.Load(ctx); err != nil {
		t.Fatal(err)
	}
	return &sRelay{app: appSvc, users: user.New(appSvc, nil), usage: usage.New(nil), prices: prices}, db
}

func atomicRecord(s *sRelay, keyID uint64, model string) error {
	return s.record(context.Background(), "atomic-request", apikey.AuthKey{Id: keyID, UserId: 1, KeyHash: "hash"}, Candidate{PublicName: model}, "", "/chat/completions", model, false, 1, time.Now(), attemptResult{status: 200})
}

func assertAtomicLedger(t *testing.T, db gdb.DB, balance, spent float64, logs int) {
	t.Helper()
	for _, check := range []struct {
		sql  string
		want float64
	}{
		{"SELECT balance FROM users WHERE id=1", balance},
		{"SELECT spent_amount FROM api_keys WHERE id=1", spent},
		{"SELECT COUNT(*) FROM usage_logs", float64(logs)},
	} {
		value, err := db.GetValue(context.Background(), check.sql)
		if err != nil || value.Float64() != check.want {
			t.Fatalf("%s = %v, want %v, err=%v", check.sql, value, check.want, err)
		}
	}
}

func TestUsageAccountingAtomicCommit(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if err := atomicRecord(s, 1, "paid"); err != nil {
		t.Fatal(err)
	}
	assertAtomicLedger(t, db, 8, 2, 1)
	value, err := db.GetValue(context.Background(), "SELECT billing_details_json FROM usage_logs")
	if err != nil {
		t.Fatal(err)
	}
	var details usage.BillingBreakdown
	if err = json.Unmarshal(value.Bytes(), &details); err != nil || !details.Charged {
		t.Fatalf("successful ledger must be charged: %+v %v", details, err)
	}
}

func TestUsageAccountingRollsBackWhenAPIKeySpendFails(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if err := atomicRecord(s, 999, "paid"); err == nil || user.IsInsufficientBalance(err) {
		t.Fatalf("missing API key must propagate infrastructure error: %v", err)
	}
	assertAtomicLedger(t, db, 10, 0, 0)
}

func TestUsageAccountingRollsBackWhenLogFails(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if _, err := db.Exec(context.Background(), "CREATE TRIGGER fail_usage BEFORE INSERT ON usage_logs BEGIN SELECT RAISE(ABORT,'injected usage failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := atomicRecord(s, 1, "paid"); err == nil || !strings.Contains(err.Error(), "injected usage failure") {
		t.Fatalf("usage persistence error must propagate: %v", err)
	}
	assertAtomicLedger(t, db, 10, 0, 0)
}

func TestUsageAccountingInsufficientBalanceCommitsRejection(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if _, err := db.Exec(context.Background(), "UPDATE users SET balance=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := atomicRecord(s, 1, "paid"); !user.IsInsufficientBalance(err) {
		t.Fatalf("expected insufficient balance: %v", err)
	}
	assertAtomicLedger(t, db, 1, 0, 1)
	status, err := db.GetValue(context.Background(), "SELECT http_status FROM usage_logs")
	if err != nil || status.Int() != 402 {
		t.Fatalf("rejection log must commit: %v %v", status, err)
	}
}

func TestUsageAccountingDebitFailureDoesNotBecome402(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if _, err := db.Exec(context.Background(), "CREATE TRIGGER fail_debit BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT,'injected debit failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := atomicRecord(s, 1, "paid"); err == nil || user.IsInsufficientBalance(err) || !strings.Contains(err.Error(), "injected debit failure") {
		t.Fatalf("debit persistence error must propagate: %v", err)
	}
	assertAtomicLedger(t, db, 10, 0, 0)
}

func TestUsageAccountingFreeRequestOnlyLogs(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	s.app = nil
	s.users = nil
	if err := atomicRecord(s, 1, "free"); err != nil {
		t.Fatal(err)
	}
	assertAtomicLedger(t, db, 10, 0, 1)
}
