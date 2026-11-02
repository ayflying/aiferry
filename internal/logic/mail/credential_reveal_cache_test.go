package mail

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
)

func TestMissingCredentialRevealCodeIsExpired(t *testing.T) {
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "mail.db"))
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: "sqlite::@file(" + path + ")"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	s := &sMail{app: &app.Service{Redis: store}}
	if err = s.VerifyCredentialRevealCode(ctx, 1, "123456"); err == nil || !strings.Contains(err.Error(), "验证码已过期") {
		t.Fatalf("missing code must report expiration, got %v", err)
	}
	if verified, verifyErr := s.CredentialRevealVerified(ctx, 1); verifyErr != nil || verified {
		t.Fatalf("missing verification must remain unverified, got %v %v", verified, verifyErr)
	}
}
