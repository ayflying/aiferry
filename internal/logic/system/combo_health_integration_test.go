package system

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 仅在显式隔离测试库启用，禁止对生产连接运行。
func TestComboHealthIsolationAndConcurrency(t *testing.T) {
	link := os.Getenv("AIFERRY_COMBO_TEST_DSN")
	if link == "" {
		t.Skip("需要显式隔离数据库连接")
	}
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Link: link}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s := &sSystem{}
	// 夹具只使用固定测试前缀，每次新建渠道避免污染并行测试。
	ch, err := dao.Channels.Ctx(ctx).Data(do.Channels{Name: "acceptance-combo-health", Type: "openai", BaseUrl: "http://127.0.0.1", ApiKeyCipher: "test-only", Status: 1, AutoDisableEnabled: 1}).InsertAndGetId()
	if err != nil {
		t.Fatal(err)
	}
	channelID := uint64(ch)
	var modelIDs, keyIDs []uint64
	for _, name := range []string{"model-a", "model-b"} {
		id, e := dao.ChannelModels.Ctx(ctx).Data(do.ChannelModels{ChannelId: channelID, PublicName: name, UpstreamName: name, Enabled: 1, HealthScore: 100}).InsertAndGetId()
		if e != nil {
			t.Fatal(e)
		}
		modelIDs = append(modelIDs, uint64(id))
	}
	for _, name := range []string{"fixture-a", "fixture-b"} {
		id, e := dao.ChannelCredentials.Ctx(ctx).Data(do.ChannelCredentials{ChannelId: channelID, KeyPrefix: name, KeyHash: name + time.Now().String(), ApiKeyCipher: "test-only", Status: 1}).InsertAndGetId()
		if e != nil {
			t.Fatal(e)
		}
		keyIDs = append(keyIDs, uint64(id))
	}
	t.Cleanup(func() {
		dao.ChannelModelCredentials.Ctx(ctx).Where(do.ChannelModelCredentials{ChannelId: channelID}).Delete()
		dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID}).Delete()
		dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{ChannelId: channelID}).Delete()
		dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID}).Delete()
	})
	settings := DefaultResilienceSettings()
	settings.DisableStatusCodes = "401,402,403,404"
	failure := ModelDisableInput{ChannelID: channelID, ModelID: modelIDs[0], ChannelCredentialID: keyIDs[0], Status: 429, Message: "rate limited"}
	for i := 0; i < 3; i++ {
		if _, e := s.DisableIfNeededWithSettings(ctx, settings, AutoDisableInput{ChannelID: channelID, ChannelModelID: modelIDs[0], ChannelCredentialID: keyIDs[0], Status: 429, Message: "rate limited"}); e != nil {
			t.Fatal(e)
		}
	}
	read := func(model, key uint64) entity.ChannelModelCredentials {
		var row entity.ChannelModelCredentials
		if e := dao.ChannelModelCredentials.Ctx(ctx).Where(do.ChannelModelCredentials{ChannelModelId: model, ChannelCredentialId: key}).Scan(&row); e != nil && !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(e)
		}
		return row
	}
	if row := read(modelIDs[0], keyIDs[0]); row.HealthScore != 70 {
		t.Fatalf("三次429应70分，得到%d", row.HealthScore)
	}
	if row := read(modelIDs[0], keyIDs[1]); row.Id != 0 {
		t.Fatal("误写其它key组合")
	}
	if row := read(modelIDs[1], keyIDs[0]); row.Id != 0 {
		t.Fatal("误写其它模型组合")
	}
	var model entity.ChannelModels
	dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelIDs[0]}).Scan(&model)
	if model.HealthScore != 100 {
		t.Fatalf("未失败的另一key应保持模型投影100，得到%d", model.HealthScore)
	}
	// 已初始化70分并发五次扣分必须20，不能丢更新。
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ApplyModelHealthScore(ctx, settings, failure); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if row := read(modelIDs[0], keyIDs[0]); row.HealthScore != 20 {
		t.Fatalf("并发扣分丢失，得到%d", row.HealthScore)
	}
	for i := 0; i < 2; i++ {
		if _, e := s.ApplyModelHealthScore(ctx, settings, failure); e != nil {
			t.Fatal(e)
		}
	}
	row := read(modelIDs[0], keyIDs[0])
	if row.HealthScore != 0 || row.CooldownUntil == nil || !row.CooldownUntil.After(gtime.Now()) {
		t.Fatal("归零未隔离")
	}
	if e := s.BumpComboHealthScore(ctx, modelIDs[0], keyIDs[0], 5); e != nil {
		t.Fatal(e)
	}
	row = read(modelIDs[0], keyIDs[0])
	if row.HealthScore != 5 || row.CooldownUntil != nil {
		t.Fatalf("恢复应5分且清冷却: %+v", row)
	}
	bad := failure
	bad.ChannelID = channelID + 1000000
	if _, e := s.ApplyModelHealthScore(ctx, settings, bad); e == nil {
		t.Fatal("错误渠道归属未拒绝")
	}
}
