package system

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
)

func TestRecoveryBeginsWithoutCachedState(t *testing.T) {
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "recovery.db"))
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + path + ")"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	s := New(&app.Service{Redis: store})
	if started, startErr := s.BeginRecoveryAttempt(ctx, RecoveryTargetChannel, 1, time.Now().Add(-time.Hour)); startErr != nil || !started {
		t.Fatalf("first recovery must start on cache miss: %v %v", started, startErr)
	}
}
