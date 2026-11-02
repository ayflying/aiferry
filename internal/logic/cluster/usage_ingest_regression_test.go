package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
)

func assertUsageCounts(t *testing.T, db gdb.DB, logs, batches int) {
	t.Helper()
	for table, want := range map[string]int{"usage_logs": logs, "cluster_usage_batches": batches} {
		got, err := db.GetCount(context.Background(), "SELECT COUNT(*) FROM "+table)
		if err != nil || got != want {
			t.Fatalf("%s count=%d err=%v, want %d", table, got, err, want)
		}
	}
}

func TestHandleUsageSameBatchDuplicateAndReplayCount(t *testing.T) {
	billing := &fixedBilling{}
	s := &Service{database: openUsageDatabase(t), billing: billing}
	input := usageBatch{BatchID: "dupe", Rows: []map[string]any{chargedRow(), chargedRow()}}
	for attempt := 0; attempt < 2; attempt++ {
		ack, err := s.HandleUsage(context.Background(), &Node{Id: 1}, input)
		if err != nil || ack.Accepted != 1 || ack.Replayed != (attempt == 1) {
			t.Fatalf("attempt %d ack=%+v err=%v", attempt, ack, err)
		}
	}
	if len(billing.debits) != 1 || len(billing.spends) != 1 {
		t.Fatalf("duplicate billed: %+v", billing)
	}
	assertUsageCounts(t, s.database, 1, 1)
}

func TestHandleUsageRejectsInvalidInputBeforeTransaction(t *testing.T) {
	cases := map[string]func(*usageBatch){
		"empty batch":          func(b *usageBatch) { b.BatchID = "" },
		"oversize batch":       func(b *usageBatch) { b.Rows = make([]map[string]any, usageBatchMaxRows+1) },
		"reserved id":          func(b *usageBatch) { b.Rows[0]["id"] = 7 },
		"reserved node":        func(b *usageBatch) { b.Rows[0]["node_id"] = 2 },
		"unknown column":       func(b *usageBatch) { b.Rows[0]["secret"] = "x" },
		"sql raw":              func(b *usageBatch) { b.Rows[0]["estimated_cost"] = gdb.Raw("1") },
		"missing request":      func(b *usageBatch) { delete(b.Rows[0], "request_id") },
		"long request":         func(b *usageBatch) { b.Rows[0]["request_id"] = string(make([]byte, 65)) },
		"request spaces":       func(b *usageBatch) { b.Rows[0]["request_id"] = " " },
		"negative user":        func(b *usageBatch) { b.Rows[0]["user_id"] = -1 },
		"fractional user":      func(b *usageBatch) { b.Rows[0]["user_id"] = 7.5 },
		"overflow user":        func(b *usageBatch) { b.Rows[0]["user_id"] = uint64(math.MaxUint64) },
		"imprecise user":       func(b *usageBatch) { b.Rows[0]["user_id"] = float64(1 << 54) },
		"missing key":          func(b *usageBatch) { delete(b.Rows[0], "api_key_id") },
		"negative cost":        func(b *usageBatch) { b.Rows[0]["estimated_cost"] = -0.1 },
		"NaN cost":             func(b *usageBatch) { b.Rows[0]["estimated_cost"] = math.NaN() },
		"Inf cost":             func(b *usageBatch) { b.Rows[0]["estimated_cost"] = math.Inf(1) },
		"overflow cost":        func(b *usageBatch) { b.Rows[0]["estimated_cost"] = "1000000000000" },
		"exponent bomb":        func(b *usageBatch) { b.Rows[0]["estimated_cost"] = json.Number("1e9999999") },
		"missing charged cost": func(b *usageBatch) { delete(b.Rows[0], "estimated_cost") },
		"invalid details":      func(b *usageBatch) { b.Rows[0]["billing_details_json"] = "{" },
		"string charged":       func(b *usageBatch) { b.Rows[0]["billing_details_json"] = `{"charged":"true"}` },
		"null charged":         func(b *usageBatch) { b.Rows[0]["billing_details_json"] = `{"charged":null}` },
		"numeric charged":      func(b *usageBatch) { b.Rows[0]["billing_details_json"] = `{"charged":1}` },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := usageBatch{BatchID: "valid", Rows: []map[string]any{chargedRow()}}
			mutate(&input)
			// nil DB proves validation completed before any I/O or debit.
			ack, err := (&Service{}).HandleUsage(context.Background(), &Node{Id: 1}, input)
			if err == nil || ack != nil {
				t.Fatalf("invalid batch got ack=%+v err=%v", ack, err)
			}
		})
	}
}

// transactionBilling 模拟装配方必须遵守的契约：两次写入均绑定 txCtx。
// AddSpend 失败时用户借记也必须一起回滚，重试才能保证恰好一次。
type transactionBilling struct {
	db   gdb.DB
	fail string
}

func (b *transactionBilling) Debit(ctx context.Context, userID uint64, amount decimal.Decimal) error {
	if _, err := b.db.Ctx(ctx).Exec(ctx, "INSERT INTO billing_entries(kind, amount) VALUES('debit', ?)", amount.String()); err != nil {
		return err
	}
	if b.fail == "debit" {
		return errors.New("debit failed")
	}
	return nil
}

func (b *transactionBilling) AddSpend(ctx context.Context, keyID uint64, amount float64) error {
	if _, err := b.db.Ctx(ctx).Exec(ctx, "INSERT INTO billing_entries(kind, amount) VALUES('spend', ?)", amount); err != nil {
		return err
	}
	if b.fail == "spend" {
		return errors.New("spend failed")
	}
	return nil
}

func TestHandleUsageBillingFailureRollsBackAndRetry(t *testing.T) {
	for _, fail := range []string{"debit", "spend", "nil"} {
		t.Run(fail, func(t *testing.T) {
			db := openUsageDatabase(t)
			ctx := context.Background()
			if _, err := db.Exec(ctx, "CREATE TABLE billing_entries(kind TEXT, amount TEXT)"); err != nil {
				t.Fatal(err)
			}
			billing := &transactionBilling{db: db, fail: fail}
			s := &Service{database: db, billing: billing}
			if fail == "nil" {
				s.billing = nil
			}
			batch := usageBatch{BatchID: "retry", Rows: []map[string]any{chargedRow()}}
			ack, err := s.HandleUsage(ctx, &Node{Id: 1}, batch)
			if err == nil || ack != nil {
				t.Fatalf("failure ACK=%+v err=%v", ack, err)
			}
			assertUsageCounts(t, db, 0, 0)
			count, err := db.GetCount(ctx, "SELECT COUNT(*) FROM billing_entries")
			if err != nil || count != 0 {
				t.Fatalf("partial billing remains %d: %v", count, err)
			}
			billing.fail, s.billing = "", billing
			ack, err = s.HandleUsage(ctx, &Node{Id: 1}, batch)
			if err != nil || ack.Accepted != 1 || ack.Replayed {
				t.Fatalf("retry ACK=%+v err=%v", ack, err)
			}
			assertUsageCounts(t, db, 1, 1)
			count, err = db.GetCount(ctx, "SELECT COUNT(*) FROM billing_entries")
			if err != nil || count != 2 {
				t.Fatalf("retry billing %d: %v", count, err)
			}
		})
	}
}

func TestHandleUsageNonDuplicateConstraintErrorRollsBack(t *testing.T) {
	s := &Service{database: openUsageDatabase(t), billing: &fixedBilling{}}
	ctx := context.Background()
	if _, err := s.database.Exec(ctx, `CREATE TRIGGER reject_usage BEFORE INSERT ON usage_logs WHEN NEW.request_id='req-2' BEGIN SELECT RAISE(ABORT, 'invalid usage'); END`); err != nil {
		t.Fatal(err)
	}
	other := chargedRow()
	other["request_id"] = "req-2"
	ack, err := s.HandleUsage(ctx, &Node{Id: 1}, usageBatch{BatchID: "bad", Rows: []map[string]any{chargedRow(), other}})
	if err == nil || ack != nil {
		t.Fatalf("constraint error ACK=%+v err=%v", ack, err)
	}
	assertUsageCounts(t, s.database, 0, 0)
}

func TestHandleUsageConcurrentBatches(t *testing.T) {
	for _, sameBatch := range []bool{true, false} {
		t.Run(fmt.Sprint(sameBatch), func(t *testing.T) {
			db := openUsageDatabase(t)
			// SQLite 单写者：多调用同时争抢同一连接；不连接真实 MySQL。
			db.SetMaxOpenConnCount(1)
			billing := &fixedBilling{}
			s := &Service{database: db, billing: billing}
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					id := "same"
					if !sameBatch {
						id = fmt.Sprintf("batch-%d", i)
					}
					ack, err := s.HandleUsage(context.Background(), &Node{Id: 1}, usageBatch{BatchID: id, Rows: []map[string]any{chargedRow()}})
					if err != nil || ack == nil {
						t.Errorf("concurrent ack=%+v err=%v", ack, err)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			batches := 12
			if sameBatch {
				batches = 1
			}
			assertUsageCounts(t, db, 1, batches)
			if len(billing.debits) != 1 || len(billing.spends) != 1 {
				t.Fatalf("concurrent billing=%+v", billing)
			}
		})
	}
}

func TestHandleUsageEmptyAndUnchargedWithoutBilling(t *testing.T) {
	s := &Service{database: openUsageDatabase(t)}
	ctx := context.Background()
	for attempt := 0; attempt < 2; attempt++ {
		ack, err := s.HandleUsage(ctx, &Node{Id: 1}, usageBatch{BatchID: "empty"})
		if err != nil || ack.Accepted != 0 || ack.Replayed != (attempt == 1) {
			t.Fatalf("empty ACK=%+v err=%v", ack, err)
		}
	}
	row := chargedRow()
	delete(row, "billing_details_json")
	ack, err := s.HandleUsage(ctx, &Node{Id: 1}, usageBatch{BatchID: "legacy", Rows: []map[string]any{row}})
	if err != nil || ack.Accepted != 1 {
		t.Fatalf("legacy uncharged ACK=%+v err=%v", ack, err)
	}
	row = chargedRow()
	row["request_id"], row["estimated_cost"] = "zero", 0.0
	ack, err = s.HandleUsage(ctx, &Node{Id: 1}, usageBatch{BatchID: "zero", Rows: []map[string]any{row}})
	if err == nil || ack != nil {
		t.Fatalf("charged zero cost still requires billing: ACK=%+v err=%v", ack, err)
	}
	assertUsageCounts(t, s.database, 1, 2)
}

func TestUsageDuplicateErrorsAreNarrow(t *testing.T) {
	for _, code := range []uint16{1062, 1048, 1264, 1406, 1452} {
		err := gerror.Wrap(&mysql.MySQLError{Number: code, Message: "duplicate"}, "insert")
		if got := usageDuplicateError(err); got != (code == 1062) {
			t.Fatalf("code %d duplicate=%v", code, got)
		}
	}
	if usageDuplicateError(errors.New("duplicate entry")) {
		t.Fatal("must not classify by message")
	}
}

func TestUsageChargedIsExplicit(t *testing.T) {
	for _, raw := range []any{nil, "", `{}`, `{"charged":false}`, `{"charged":"true"}`, `{broken`} {
		row := chargedRow()
		row["billing_details_json"] = raw
		if _, charged := usageCostOf(row); charged {
			t.Fatalf("invalid/missing charged billed: %v", raw)
		}
	}
}

func TestHandleUsageBodyPreservesIdentityPrecision(t *testing.T) {
	db := openUsageDatabase(t)
	s := &Service{database: db, billing: &fixedBilling{}}
	body := []byte(`{"batchId":"precise","rows":[{"request_id":"precise","user_id":9007199254740993,"api_key_id":21,"estimated_cost":1e-8,"billing_details_json":"{\"charged\":false}"}]}`)
	ack, err := s.HandleUsageBody(context.Background(), &Node{Id: 1}, body)
	if err != nil || ack.Accepted != 1 {
		t.Fatalf("precise ack=%+v err=%v", ack, err)
	}
	value, err := db.GetValue(context.Background(), "SELECT user_id FROM usage_logs")
	if err != nil || value.Uint64() != 9007199254740993 {
		t.Fatalf("lost identity precision: %v %v", value, err)
	}
	for _, invalid := range []string{`{"batchId":"x","rows":[],"extra":1}`, `{"batchId":"x","rows":[]} {}`} {
		if ack, err := s.HandleUsageBody(context.Background(), &Node{Id: 1}, []byte(invalid)); err == nil || ack != nil {
			t.Fatalf("invalid body ACK=%+v err=%v", ack, err)
		}
	}
}
