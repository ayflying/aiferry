package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
)

func TestMissingCachePreservesAuthenticationErrors(t *testing.T) {
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "auth.db"))
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + path + ")"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	s := New(&app.Service{Redis: store}, nil)
	if _, err = s.Authenticate(ctx, "missing-session"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("missing session must be unauthorized, got %v", err)
	}
	if _, _, _, err = s.CompleteLogin(ctx, "missing-state", "missing-state", "code"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("missing OAuth state must be invalid state, got %v", err)
	}
}
