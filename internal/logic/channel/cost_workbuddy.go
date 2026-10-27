package channel

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// WorkBuddy 没有 OpenAI 风格的 /organization/costs 接口，桌面端只提供
// get-user-resource-summary（见 quota_workbuddy.go）这一个资源汇总接口，
// 返回的是每个套餐的「周期总量 / 周期已用 / 周期剩余」积分。
//
// 0.6.36 起把它同时接进「费用与余额」这条通用链路（costs.adapter =
// workbuddy_credits），原因是：积分本质就是可消费额度，管理端密钥表里的
// 「费用与余额」列、渠道列表的余额汇总和余额提醒都读 last_cost_* 这些通用字段。
// 只走套餐额度弹窗时，用户在同一张表里看到的是「尚未查询」，两个入口各说一套，
// 无法满足「在密钥列表里直接查询积分余额」的诉求。
//
// 口径（两者都是计数值，展示单位统一为「积分」）：
//
//	余额 = Σ CycleRemainCapacity
//	已用 = Σ CycleUsedCapacity（缺失时用 总量 − 剩余 补齐）
//
// 不引入任何货币符号与汇率：积分不是钱，套用 USD/CNY 折算会得出误导性金额。

// costUsageCurrency 是计数口径（用量、积分、Token）在通用费用字段里的币种标记。
// 沿用 ISO 4217 的 XXX（「无货币」）而不是自造字符串，是为了让已有的折算逻辑把
// 它当未知币种原样展示，不会误乘汇率，也不会触发金额类提醒。
const costUsageCurrency = "XXX"

// workBuddyCreditsCurrency 是 WorkBuddy 积分在通用费用字段里的币种标记。
const workBuddyCreditsCurrency = costUsageCurrency

// queryWorkBuddyCosts 用推理密钥查询单个账号的积分余额，映射为通用费用结构。
// 响应解析复用 parseWorkBuddyCredits，保证「费用与余额」和「套餐额度」两个入口
// 拿到的是同一份上游数据、同一套窗口定义，不会出现两处算法漂移。
func (s *sChannel) queryWorkBuddyCosts(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	// 与 WorkBuddy 其它请求一致：接口带 /v2 版本前缀，鉴权头由类型配置决定，
	// 未配置时回退 Bearer（桌面端实际使用的形状）。
	headerPrefix := strings.TrimSpace(config.HeaderPrefix)
	if headerPrefix == "" {
		headerPrefix = "Bearer "
	}
	body, err := s.fetchUpstreamJSON(ctx, channel, credentialCipher, upstreamJSONRequest{
		Method:       costWorkBuddyMethod(config.Method),
		Endpoint:     workBuddyCreditsEndpointURL(channel),
		AuthType:     costWorkBuddyAuthType(config.AuthType),
		HeaderName:   config.HeaderName,
		HeaderPrefix: headerPrefix,
		BodyLimit:    1 << 20,
		RequestError: "创建 WorkBuddy 积分余额查询请求失败",
		FetchError:   "请求 WorkBuddy 积分余额接口失败",
		ReadError:    "读取 WorkBuddy 积分余额接口响应失败",
		InvalidError: "WorkBuddy 积分余额接口返回了无效 JSON",
		StatusError: func(status int, payload []byte) error {
			return gerror.Newf("WorkBuddy 积分余额接口返回 HTTP %d", status)
		},
	})
	if err != nil {
		return err
	}
	view, err := parseWorkBuddyCredits(body)
	if err != nil {
		return err
	}
	used, remaining := sumWorkBuddyWindows(view)
	result.Currency = workBuddyCreditsCurrency
	result.UsedAmount = used
	result.RemainingAmount = remaining
	return nil
}

// sumWorkBuddyWindows 把套餐窗口累加成「已用 / 余额」两个数。COUNT 型窗口
// （每日签到）没有 Used/Total，返回 nil 会跳过，不会污染积分合计。
func sumWorkBuddyWindows(view QuotaView) (used, remaining *float64) {
	for _, window := range view.Windows {
		if window.Counts {
			continue
		}
		used = addQuotaValue(used, window.Used)
		remaining = addQuotaValue(remaining, window.Remaining)
	}
	return used, remaining
}

// workBuddyCreditsEndpointURL 拼出积分接口地址。渠道根地址带 /v2 版本前缀
// （https://www.workbuddy.cn/v2），而该接口只存在于 host 根之下；直接拼会得到
// /v2/billing/... 这种上游不认的地址。
//
// 实现复用 resolveHostURL：它正是 quota 适配器查同一接口时用的解析器，保证
// 「查询积分余额」与「套餐额度」两个入口永远请求同一个地址，不会一处改了另
// 一处忘改。解析失败（渠道根地址不是绝对 HTTP(S) URL）时返回空串，由调用方
// fetchUpstreamJSON 报出可读错误。
func workBuddyCreditsEndpointURL(channel entity.Channels) string {
	endpoint, err := resolveHostURL(channel.BaseUrl, workBuddyCreditsPath)
	if err != nil {
		return ""
	}
	return endpoint
}

// costWorkBuddyMethod 默认 POST：桌面端的资源汇总接口只有 POST。
func costWorkBuddyMethod(value string) string {
	if value = strings.ToUpper(strings.TrimSpace(value)); value != "" {
		return value
	}
	return "POST"
}

// costWorkBuddyAuthType 默认 channel_key：积分接口用推理密钥（accessToken）鉴权。
func costWorkBuddyAuthType(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return channeltype.AuthChannelKey
}
