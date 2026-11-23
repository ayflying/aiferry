package mail

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/system"
)

func TestAvailabilityMailThrottle(t *testing.T) {
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
	first := &sMail{app: &app.Service{Redis: store}}
	second := &sMail{app: &app.Service{Redis: store}}
	event := system.AutoDisableNotification{Kind: system.NotificationKindModelAvailability, ChannelID: 9, ModelID: 849}
	if !first.allowAvailabilityMail(ctx, event) || second.allowAvailabilityMail(ctx, event) {
		t.Fatal("同一模型的重复故障只允许首封邮件")
	}
	event.Recovered = true
	if !second.allowAvailabilityMail(ctx, event) || first.allowAvailabilityMail(ctx, event) {
		t.Fatal("首封恢复独立放行，重复恢复被拦截")
	}
	event.Recovered = false
	if second.allowAvailabilityMail(ctx, event) {
		t.Fatal("恢复不应重置故障发送窗口")
	}
	other := event
	other.ModelID++
	if !first.allowAvailabilityMail(ctx, other) {
		t.Fatal("其他模型的邮件不应被拦截")
	}
	manual := event
	manual.Manual = true
	if !first.allowAvailabilityMail(ctx, manual) || !second.allowAvailabilityMail(ctx, manual) {
		t.Fatal("手动事件不受自动告警限频影响")
	}
	channel := event
	channel.Kind = system.NotificationKindChannelAvailability
	if !first.allowAvailabilityMail(ctx, channel) {
		t.Fatal("模型告警不应挡住整个渠道不可用的首封告警")
	}
	channel.ModelID++
	if second.allowAvailabilityMail(ctx, channel) {
		t.Fatal("整个渠道告警不应按触发模型分别发送")
	}
	if err := store.Del(ctx, availabilityMailKey(event)).Err(); err != nil {
		t.Fatal(err)
	}
	if !second.allowAvailabilityMail(ctx, event) {
		t.Fatal("限频窗口不在时应允许下一封故障告警")
	}
}
