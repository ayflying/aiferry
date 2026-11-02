package user

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
)

// DAO 使用 g.DB("default")，GoFrame 另有进程级实例缓存。子进程隔离不仅恢复
// 配置，还避免把已关闭的 default 实例留给其他测试；不加载应用配置或 .env。
func TestSQLiteUserSettlement(t *testing.T) {
	const marker = "AIFERRY_TEST_USER_SETTLEMENT_CHILD"
	if os.Getenv(marker) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-test.run=^TestSQLiteUserSettlement$", "-test.v")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated SQLite settlement: %v\n%s", err, output)
		}
		return
	}
	ctx := context.Background()
	adapter, err := gcfg.NewAdapterContent("{}")
	if err != nil {
		t.Fatal(err)
	}
	g.Cfg().SetAdapter(adapter)
	if err := gdb.SetConfig(gdb.Config{}); err != nil {
		t.Fatal(err)
	}
	appSvc, err := app.New(ctx, config.App{
		MasterKey: []byte(strings.Repeat("k", 32)),
		Cluster:   config.Cluster{Role: config.RoleWorker, DataDir: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	db := g.DB("default")
	workerDB, err := gdb.Instance()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close(ctx)
		_ = workerDB.Close(ctx)
	})
	// 多个真实连接争抢写锁，不能用单连接掩盖 SQLite 读锁升级问题。
	db.SetMaxOpenConnCount(8)
	s := New(appSvc, nil)
	reset := func(t *testing.T, balance string) {
		t.Helper()
		if _, err := db.Exec(ctx, "DELETE FROM users"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, "INSERT INTO users(id,name,role,status,balance) VALUES(7,'alice','user',1,?)", balance); err != nil {
			t.Fatal(err)
		}
	}
	assertBalance := func(t *testing.T, want string) {
		t.Helper()
		value, err := db.GetValue(ctx, "SELECT balance FROM users WHERE id=7")
		if err != nil || value.IsNil() || !decimal.NewFromFloat(value.Float64()).Round(8).Equal(decimal.RequireFromString(want)) {
			t.Fatalf("balance=%v err=%v, want %s", value, err, want)
		}
	}
	t.Run("ordinary debit cannot overdraw but settlement records debt", func(t *testing.T) {
		reset(t, "0.25")
		if err := s.Debit(ctx, 7, decimal.RequireFromString("0.5")); !IsInsufficientBalance(err) {
			t.Fatalf("overdraft error=%v", err)
		}
		assertBalance(t, "0.25")
		if err := s.SettleUsage(ctx, 7, decimal.RequireFromString("0.5")); err != nil {
			t.Fatal(err)
		}
		assertBalance(t, "-0.25")
		if err := s.Debit(ctx, 7, decimal.RequireFromString("0.01")); !IsInsufficientBalance(err) {
			t.Fatalf("debt must reject ordinary debit: %v", err)
		}
		if err := s.SettleUsage(ctx, 7, decimal.RequireFromString("0.5")); err != nil {
			t.Fatal(err)
		}
		assertBalance(t, "-0.75")
	})
	t.Run("exact debit and eight decimal rounding", func(t *testing.T) {
		reset(t, "0.3")
		for _, amount := range []string{"0.100000004", "0.2"} {
			if err := s.Debit(ctx, 7, decimal.RequireFromString(amount)); err != nil {
				t.Fatal(err)
			}
		}
		assertBalance(t, "0")
	})
	for _, settle := range []bool{false, true} {
		name := "Debit"
		if settle {
			name = "SettleUsage"
		}
		t.Run(name+" respects outer rollback", func(t *testing.T) {
			reset(t, "1")
			rollback := errors.New("caller requested rollback")
			err := db.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
				var debitErr error
				if settle {
					debitErr = s.SettleUsage(txCtx, 7, decimal.NewFromInt(2))
				} else {
					debitErr = s.Debit(txCtx, 7, decimal.RequireFromString("0.25"))
				}
				if debitErr != nil {
					return debitErr
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("outer transaction error=%v", err)
			}
			assertBalance(t, "1")
		})
	}
	t.Run("concurrent settlement preserves all debt", func(t *testing.T) {
		reset(t, "0.5")
		const count = 8
		start := make(chan struct{})
		results := make(chan error, count)
		var wg sync.WaitGroup
		for range count {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- s.SettleUsage(ctx, 7, decimal.RequireFromString("0.25"))
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Errorf("concurrent settlement: %v", err)
			}
		}
		assertBalance(t, "-1.5")
	})
	t.Run("concurrent ordinary debit cannot overspend", func(t *testing.T) {
		reset(t, "0.5")
		const count = 8
		start := make(chan struct{})
		results := make(chan error, count)
		var wg sync.WaitGroup
		for range count {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- s.Debit(ctx, 7, decimal.RequireFromString("0.25"))
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !IsInsufficientBalance(err) {
				t.Errorf("unexpected concurrent debit failure: %v", err)
			}
		}
		if successes != 2 {
			t.Fatalf("successful debits=%d, want 2", successes)
		}
		assertBalance(t, "0")
	})
}
