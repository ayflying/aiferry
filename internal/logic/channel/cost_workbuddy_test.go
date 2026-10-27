package channel

import (
	"strings"
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

// TestWorkBuddyStatusPreview 守住日志片段：上游 401 常常返回空 body，
// 日志里要能区分「空响应」与「有内容但被截断」，且不把整页 HTML 灌进日志。
func TestWorkBuddyStatusPreview(t *testing.T) {
	if got := workBuddyStatusPreview(nil); got != "(empty)" {
		t.Fatalf("空 body 应标记为 (empty)，got %q", got)
	}
	if got := workBuddyStatusPreview([]byte("  \n\t ")); got != "(empty)" {
		t.Fatalf("纯空白 body 应标记为 (empty)，got %q", got)
	}
	if got := workBuddyStatusPreview([]byte("{\n  \"error_msg\": \"401\"\n}")); got != `{ "error_msg": "401" }` {
		t.Fatalf("换行应压成空格，got %q", got)
	}
	long := strings.Repeat("a", 400)
	got := workBuddyStatusPreview([]byte(long))
	if len([]rune(got)) != 161 || !strings.HasSuffix(got, "…") {
		t.Fatalf("超长 body 应截断到 160 字并加省略号，got 长度 %d", len([]rune(got)))
	}
}

// TestLocalizedCostQueryErrorKeepsGatewayVerdict 守住 401 的措辞优先级。
//
// 2026-10-27 生产实测：workbuddy 积分接口被上游网关拒绝（HTTP 401），而同一把
// 密钥在 /v2/billing/meter/checkin-activity-status 签到接口上是可用的，说明门槛
// 在网关侧而不是「密钥无效」。通用 401 文案会把结论改写成「密钥失效」，把排查
// 方向带偏，所以网关结论必须原样保留。
func TestLocalizedCostQueryErrorKeepsGatewayVerdict(t *testing.T) {
	gateway := "WorkBuddy 积分接口返回 HTTP 401（上游网关拒绝：该接口需要网页登录会话，或该账号被平台风控限流；同一账号的签到接口可用，可稍后重试）"
	if got := localizedCostQueryError(gateway); got != gateway {
		t.Fatalf("网关拒绝结论被改写：%q", got)
	}
	generic := "WorkBuddy 积分余额接口返回 HTTP 401"
	if got := localizedCostQueryError(generic); !strings.Contains(got, "密钥无效") {
		t.Fatalf("普通 401 仍应提示密钥可能失效，got %q", got)
	}
}
