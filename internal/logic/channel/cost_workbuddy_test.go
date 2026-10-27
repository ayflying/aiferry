package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// TestWorkBuddyCostsAdapterDeclared 守住「WorkBuddy 能用通用的查询余额/积分入口」。
//
// 背景（2026-10-27 用户诉求）：密钥抽屉里只有「查询费用」一个按钮，而 workbuddy
// 当时的 costs.adapter 是 none，按钮被禁用，表格里永远是「尚未查询」。积分余额
// 因此只能从套餐额度弹窗里看，两个入口各说一套。这里钉住三件事：
//  1. 必须声明 workbuddy_credits 费用适配器；
//  2. 必须按计数口径（usage）记账，积分不能当金额去乘汇率；
//  3. 适配器必须通过渠道类型校验，否则保存配置会直接失败。
func TestWorkBuddyCostsAdapterDeclared(t *testing.T) {
	config, err := channeltype.ParseConfig(testBuiltinsConfig(t, "workbuddy"))
	if err != nil {
		t.Fatalf("解析 workbuddy 内置配置失败：%v", err)
	}
	if config.Costs.Adapter != channeltype.AdapterWorkBuddy {
		t.Fatalf("workbuddy costs.adapter = %q, want %q", config.Costs.Adapter, channeltype.AdapterWorkBuddy)
	}
	if !channeltype.IsUsageCost(config.Costs) {
		t.Fatalf("workbuddy costs 必须按计数口径处理，valueType = %q", config.Costs.ValueType)
	}
	if config.Costs.Method != "POST" {
		t.Fatalf("workbuddy costs.method = %q, want POST", config.Costs.Method)
	}
	if config.Costs.AuthType != channeltype.AuthChannelKey {
		t.Fatalf("workbuddy costs.authType = %q, want channel_key", config.Costs.AuthType)
	}
}

// TestWorkBuddyCostsValueTypeDefaultsToUsage 守住「清单漏写 valueType 也不会被
// 当成金额」：默认 valueType 是 cost，漏写会让余额汇总把积分当美元折算。
func TestWorkBuddyCostsValueTypeDefaultsToUsage(t *testing.T) {
	config, err := channeltype.ParseConfig([]byte(`{"costs":{"adapter":"workbuddy_credits","method":"POST","path":"/billing/meter/get-user-resource-summary"}}`))
	if err != nil {
		t.Fatalf("解析配置失败：%v", err)
	}
	if config.Costs.ValueType != channeltype.ValueTypeUsage {
		t.Fatalf("valueType = %q, want usage", config.Costs.ValueType)
	}
}

// TestSumWorkBuddyWindows 校验积分合计口径：多套餐累加，签到这类计数窗口不参与。
func TestSumWorkBuddyWindows(t *testing.T) {
	body := []byte(`{"code":0,"data":{"Packages":[` +
		`{"PackageCode":"A","CycleTotalCapacity":1000,"CycleRemainCapacity":900,"CycleUsedCapacity":100},` +
		`{"PackageCode":"B","CycleTotalCapacity":500,"CycleRemainCapacity":200,"CycleUsedCapacity":300}]}}`)
	credits, err := parseWorkBuddyCredits(body)
	if err != nil {
		t.Fatalf("parseWorkBuddyCredits error: %v", err)
	}
	checkin, err := parseWorkBuddyCheckin([]byte(`{"code":0,"data":{"today_checked_in":true}}`))
	if err != nil {
		t.Fatalf("parseWorkBuddyCheckin error: %v", err)
	}
	views := credits
	views.Windows = append(views.Windows, checkin.Windows...)
	used, remaining := sumWorkBuddyWindows(views)
	if used == nil || *used != 400 {
		t.Fatalf("used = %v, want 400", used)
	}
	if remaining == nil || *remaining != 1100 {
		t.Fatalf("remaining = %v, want 1100", remaining)
	}
}

// TestUpstreamHTTPStatus 校验从错误文案恢复上游状态码：拿不到时返回 0，
// 否则自动禁用会把网络错误误判成鉴权失败。
func TestUpstreamHTTPStatus(t *testing.T) {
	cases := []struct {
		message string
		want    int
	}{
		{"WorkBuddy 积分余额接口返回 HTTP 401", 401},
		{"上游接口返回 HTTP 402：insufficient balance", 402},
		{"请求 WorkBuddy 积分余额接口失败：dial tcp timeout", 0},
		{"HTTP 调用超时", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := upstreamHTTPStatus(tc.message); got != tc.want {
			t.Fatalf("upstreamHTTPStatus(%q) = %d, want %d", tc.message, got, tc.want)
		}
	}
}

// TestCostQuerySkipAutoDisable 守住 WorkBuddy 不参与查询触发的自动禁用：
// 它的推理密钥是会自动续期的 accessToken，禁用账号反而让用户用不了。
func TestCostQuerySkipAutoDisable(t *testing.T) {
	if !costQuerySkipAutoDisable(channeltype.AdapterWorkBuddy) {
		t.Fatal("workbuddy 查询失败不应触发自动禁用")
	}
	if costQuerySkipAutoDisable(channeltype.AdapterOpenAICosts) {
		t.Fatal("OpenAI 费用查询失败应参与自动禁用判定")
	}
}

// TestWorkBuddyCreditsEndpointKeepsHostRoot 校验积分接口地址拼接：渠道根地址带
// /v2，而接口在 host 根之下，直接拼接会变成 /v2/billing/... 请求失败。
func TestWorkBuddyCreditsEndpointKeepsHostRoot(t *testing.T) {
	want := "https://www.workbuddy.cn" + workBuddyCreditsPath
	channel := entity.Channels{BaseUrl: "https://www.workbuddy.cn/v2/"}
	if got := workBuddyCreditsEndpointURL(channel); got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}
