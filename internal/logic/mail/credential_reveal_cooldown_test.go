package mail

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
)

// 发送冷却必须能被状态接口如实读出：前端只在用户点击时才发码，
// 重开弹窗靠这个剩余秒数恢复倒计时，读数错了就会出现「可点但被拒」。
func TestCredentialRevealCooldownSecondsFollowsCacheTTL(t *testing.T) {
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

	if got := s.credentialRevealCooldownSeconds(ctx, 7); got != 0 {
		t.Fatalf("missing cooldown key must report 0, got %d", got)
	}

	if err = store.Set(ctx, credentialRevealCooldownKey(7), "1", credentialRevealSendCooldown).Err(); err != nil {
		t.Fatal(err)
	}
	want := int64(credentialRevealSendCooldown.Seconds())
	got := s.credentialRevealCooldownSeconds(ctx, 7)
	if got < want-1 || got > want+1 {
		t.Fatalf("cooldown seconds = %d, want about %d", got, want)
	}

	// 冷却只对本人生效，别的用户仍可立即发送。
	if other := s.credentialRevealCooldownSeconds(ctx, 8); other != 0 {
		t.Fatalf("another user must not inherit the cooldown, got %d", other)
	}
}
