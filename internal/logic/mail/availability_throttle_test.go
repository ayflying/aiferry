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

// newThrottleTestMail 建两个共用同一份缓存的邮件服务，用来模拟并发事件。
func newThrottleTestMail(t *testing.T) (*sMail, *sMail) {
	t.Helper()
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
	return &sMail{app: &app.Service{Redis: store}}, &sMail{app: &app.Service{Redis: store}}
}

func TestAvailabilityMailThrottle(t *testing.T) {
	ctx := context.Background()
	first, second := newThrottleTestMail(t)
	event := system.AutoDisableNotification{Kind: system.NotificationKindModelAvailability, ChannelID: 9, ModelID: 849}
	if !first.allowAvailabilityMail(ctx, event).Allow || second.allowAvailabilityMail(ctx, event).Allow {
		t.Fatal("同一模型的重复故障只允许首封邮件")
	}
	event.Recovered = true
	if !second.allowAvailabilityMail(ctx, event).Allow || first.allowAvailabilityMail(ctx, event).Allow {
		t.Fatal("首封恢复独立放行，重复恢复被拦截")
	}
	event.Recovered = false
	if second.allowAvailabilityMail(ctx, event).Allow {
		t.Fatal("恢复不应重置故障发送窗口")
	}
	other := event
	other.ModelID++
	if !first.allowAvailabilityMail(ctx, other).Allow {
		t.Fatal("其他模型的邮件不应被拦截")
	}
	manual := event
	manual.Manual = true
	if !first.allowAvailabilityMail(ctx, manual).Allow || !second.allowAvailabilityMail(ctx, manual).Allow {
		t.Fatal("手动事件不受自动告警限频影响")
	}
}

// 密钥级与旧口径通知同样受限频约束，不能绕过。
func TestAvailabilityMailThrottleCoversCredentialAndLegacy(t *testing.T) {
	ctx := context.Background()
	first, second := newThrottleTestMail(t)
	credential := system.AutoDisableNotification{Kind: system.NotificationKindCredentialStatus, ChannelID: 9, CredentialID: 55}
	if !first.allowAvailabilityMail(ctx, credential).Allow || second.allowAvailabilityMail(ctx, credential).Allow {
		t.Fatal("同一把密钥的重复状态变化只允许首封邮件")
	}
	legacy := system.AutoDisableNotification{ChannelID: 9, ModelID: 849}
	if !first.allowAvailabilityMail(ctx, legacy).Allow || second.allowAvailabilityMail(ctx, legacy).Allow {
		t.Fatal("旧口径模型事件必须按模型维度限频")
	}
	legacy.ChannelID = 10
	if !second.allowAvailabilityMail(ctx, legacy).Allow {
		t.Fatal("不同渠道的同名模型互不影响")
	}
}

// 多模型同时抖动时，渠道级预算给出每小时总量上限。
func TestAvailabilityMailChannelBudget(t *testing.T) {
	ctx := context.Background()
	first, _ := newThrottleTestMail(t)
	allowed := 0
	for modelID := uint64(1); modelID <= availabilityChannelBudget+3; modelID++ {
		if first.allowAvailabilityMail(ctx, system.AutoDisableNotification{
			Kind: system.NotificationKindModelAvailability, ChannelID: 9, ModelID: modelID,
		}).Allow {
			allowed++
		}
	}
	if allowed != availabilityChannelBudget {
		t.Fatalf("渠道预算应放行 %d 封，实际 %d 封", availabilityChannelBudget, allowed)
	}
	// 恢复方向同样计入渠道预算，避免故障与恢复各走一套额度把总量放大一倍。
	if first.allowAvailabilityMail(ctx, system.AutoDisableNotification{
		Kind: system.NotificationKindModelAvailability, ChannelID: 9, ModelID: 99, Recovered: true,
	}).Allow {
		t.Fatal("渠道预算耗尽后恢复邮件也应被拦截")
	}
	// 其他渠道不受影响。
	if !first.allowAvailabilityMail(ctx, system.AutoDisableNotification{
		Kind: system.NotificationKindModelAvailability, ChannelID: 10, ModelID: 1,
	}).Allow {
		t.Fatal("渠道预算必须按渠道独立计算")
	}
}
