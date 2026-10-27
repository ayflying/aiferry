package channel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// testBuiltinsConfig 读出仓库里真实的 builtins.json 中某个渠道类型的 config 段。
// 用真实清单而不是手写夹具：本次要守住的正是「清单里必须声明某段配置」，手写
// 夹具会让测试永远通过，起不到守门作用。
func testBuiltinsConfig(t *testing.T, code string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatalf("读取 manifest/builtins.json 失败：%v", err)
	}
	items := gjson.GetBytes(raw, "channelTypes")
	if !items.IsArray() {
		t.Fatal("builtins.json 缺少 channelTypes 数组")
	}
	for _, item := range items.Array() {
		if item.Get("code").String() != code {
			continue
		}
		return []byte(item.Get("config").Raw)
	}
	t.Fatalf("builtins.json 里找不到渠道类型 %s", code)
	return nil
}

// TestWorkBuddyQuotaAdapterDeclared 守住 workbuddy 渠道类型必须声明积分额度适配器。
// 管理端「按账号查积分」的入口由 quotaSupported 驱动，而它来自类型配置里的
// quota.adapter；一旦清单漏掉这段，前端只会显示「查询费用」，用户看不到任何
// 查积分入口（2026-10-27 的现场问题）。
func TestWorkBuddyQuotaAdapterDeclared(t *testing.T) {
	config, err := channeltype.ParseConfig(testBuiltinsConfig(t, "workbuddy"))
	if err != nil {
		t.Fatalf("解析 workbuddy 内置配置失败：%v", err)
	}
	if config.Quota.Adapter != channeltype.AdapterWorkBuddy {
		t.Fatalf("workbuddy quota.adapter = %q, want %q", config.Quota.Adapter, channeltype.AdapterWorkBuddy)
	}
	// 0.6.36 起 workbuddy 的「查询费用」按钮改为查积分余额（costs.adapter =
	// workbuddy_credits）。仍然不能落到 DefaultConfig 兜底的 openai_costs +
	// management_key，否则会报「需要管理密钥」——一个和真实原因无关的错误
	// （2026-10-27 现场问题）。
	if config.Costs.Adapter != channeltype.AdapterWorkBuddy {
		t.Fatalf("workbuddy costs.adapter = %q, want %q", config.Costs.Adapter, channeltype.AdapterWorkBuddy)
	}
	if isCostAdapterUnset("volcengine_afp") {
		t.Fatal("已配置的适配器不能被判为未配置")
	}
}

// TestWorkBuddyCreditsParsedPerCredential 校验单账号积分解析：每个账号各自一份
// 响应，都能解析出窗口，互不依赖。
func TestWorkBuddyCreditsParsedPerCredential(t *testing.T) {
	bodies := []string{
		`{"code":0,"data":{"packages":[{"cycleTotalCapacity":1000,"cycleUsedCapacity":250}]}}`,
		`{"code":0,"data":{"packages":[{"cycleTotalCapacity":500,"cycleUsedCapacity":500}]}}`,
	}
	for index, body := range bodies {
		view, err := parseWorkBuddyCredits([]byte(body))
		if err != nil {
			t.Fatalf("第 %d 个账号解析失败：%v", index+1, err)
		}
		if len(view.Windows) == 0 {
			t.Fatalf("第 %d 个账号未解析出额度窗口", index+1)
		}
	}
}
