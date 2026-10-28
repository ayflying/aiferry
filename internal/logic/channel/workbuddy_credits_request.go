package channel

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// WorkBuddy 积分接口的请求特征选择。
//
// WHY。2026-10-28 用户反馈：早期版本的 AiFerry 能查到 WorkBuddy 积分余额，
// 后来查不到了。定位到 0.6.27（2026-10-27）给「所有」WorkBuddy 上游请求注入
// 官方 chat 客户端身份头（ApplyWorkBuddyOfficialHeaders），其中 Origin/Referer
// 被固定写成 https://www.codebuddy.cn，而积分接口实际打在 https://www.workbuddy.cn。
// openresty 会话门按请求的 Origin/Referer 与自身站点比对，跨站即拒绝：
// 生产日志里的「401 Authorization Required … openresty」正是这道门的 HTML 页，
// 换路径、换密钥、加 Cookie、加 X-Device-Token 都不会改变结果（2026-10-28 实测四种
// 请求头组合全部返回同一个 HTML 401）。
//
// 因此这里按两种请求特征**依次尝试**，而不是继续猜路径：
//
//	1. browser-origin：与本接口同源的浏览器 XHR 形态（不带 chat 身份头）
//	2. official-chat：0.6.27 之后的 chat 身份形态（回退，保持现状不退化）
//
// 成功的一次会写日志，使「上游接受哪种特征」在生产上可见；两种都失败时，
// 错误信息带上每种形态的上游状态，一次部署就能看清边界。
//
// 2026-10-28 实测结论（0.6.44 已证伪“跨站 Origin 是原因”）：两种形态在生产上
// 都返回同一个 HTML 401，说明这道门不看请求头形态，而看**网页登录会话**。
// 用登录态的浏览器抓包看到的事实：
//   - 网页端 POST https://www.workbuddy.cn/billing/meter/checkin-status 返回 200
//   - 该请求带 cookie，**完全没有 Authorization 头**
//   - 同一批探针里 /v2 下不存在的路由返回 404 text/plain，
//     而 /v2/billing/meter/get-user-resource 返回 HTML 401 —— 路由存在但拒绝密钥
// 也就是说：host 根 /billing/* 与网页共用一套 Cookie 会话；渠道密钥（accessToken）
// 能查签到与用量，却拿不到这份会话，因此积分余额对服务端不可查询。
// 这条结论决定了下面的错误文案：不再猜测“风控限流”，而是如实说明会话边界。

const (
	// workBuddyCreditsBrowserOrigin 是积分接口所在站点，用作 Origin/Referer。
	// 必须与请求主机一致：跨站会在会话门前被拒（见上方 WHY）。
	workBuddyCreditsBrowserOrigin = "https://www.workbuddy.cn"
	// workBuddyCreditsBrowserUA 是浏览器形态的 User-Agent（官网额度页同款）。
	workBuddyCreditsBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

// workBuddyChatIdentityHeaders 是 chat 契约专属的身份头。浏览器形态的积分请求
// 必须把它们清掉：这些头属于另一个站点域的 chat 契约，混进额度请求会让上游
// 同时看到两种身份，比缺失更容易被会话门拒绝。
var workBuddyChatIdentityHeaders = []string{
	"X-Conversation-ID",
	"X-Conversation-Request-ID",
	"X-Conversation-Message-ID",
	"X-Request-ID",
	"X-Product",
	"X-No-User-Id",
	"X-No-Enterprise-Id",
	"X-No-Department-Info",
}

// applyWorkBuddyCreditsBrowserHeaders 把积分请求改写成 WorkBuddy 官网额度页发出的
// 普通 XHR：与本接口同源的 Origin/Referer、浏览器 UA、JSON 接受类型，
// 并清除 chat 身份头。
func applyWorkBuddyCreditsBrowserHeaders(target http.Header) {
	if target == nil {
		return
	}
	for _, name := range workBuddyChatIdentityHeaders {
		target.Del(name)
	}
	target.Set("User-Agent", workBuddyCreditsBrowserUA)
	target.Set("Accept", "application/json, text/plain, */*")
	target.Set("Accept-Language", "zh-CN,zh;q=0.9")
	target.Set("X-Requested-With", "XMLHttpRequest")
	target.Set("Origin", workBuddyCreditsBrowserOrigin)
	target.Set("Referer", workBuddyCreditsBrowserOrigin+"/")
}

// workBuddyCreditsRequestVariant 描述一种积分请求特征。
type workBuddyCreditsRequestVariant struct {
	// Name 出现在日志与错误信息里，用来区分是哪一种特征被接受/拒绝。
	Name string
	// SkipIdentity 为真时不注入 WorkBuddy chat 身份头。
	SkipIdentity bool
	// Apply 在鉴权头之后追加本形态的请求头；为空表示沿用默认身份头。
	Apply func(http.Header)
}

// workBuddyCreditsRequestVariants 是候选请求形态，顺序即尝试顺序。
var workBuddyCreditsRequestVariants = []workBuddyCreditsRequestVariant{
	{Name: "browser-origin", SkipIdentity: true, Apply: applyWorkBuddyCreditsBrowserHeaders},
	{Name: "official-chat", SkipIdentity: false, Apply: nil},
}

// workBuddyCreditsRequest 是一次积分查询请求的模板。
type workBuddyCreditsRequest struct {
	// Base 是各形态共用的请求参数：方法、地址、鉴权与错误文案。
	// 其中的 Headers 与 SkipClientIdentity 由本文件的形态表覆盖，调用方不必设置。
	Base upstreamJSONRequest
}

// workBuddyCreditsRejectedText 是积分接口被上游会话门拒绝时的用户可读说明。
// 额度弹窗与密钥列表的查询按钮共用同一段文案，避免两个入口说法不一致。
//
// 措辞依据 2026-10-28 的实测（见文件头 WHY），不写“风控限流”这类猜测：
// 网页端能用是因为它带网页登录会话（Cookie），渠道密钥没有这份会话。
func workBuddyCreditsRejectedText(status int) string {
	return fmt.Sprintf("WorkBuddy 积分余额接口返回 HTTP %d：该接口只对网页登录会话开放"+
		"（网页端同源请求只带 Cookie、不带密钥头即可读余额），渠道密钥能查签到与用量，"+
		"但拿不到这份会话，所以服务端查不到余额；请到 https://www.workbuddy.cn 查看余额。"+
		"签到与用量不受影响，无需重复重试", status)
}

// fetchWorkBuddyCreditsBody 按 workBuddyCreditsRequestVariants 依次尝试查询积分
// 接口，返回第一次成功的响应体。全部形态都失败时，错误里带上每种形态的上游状态，
// 便于一次性定位，而不是只看到最后一种的回退结果。
func (s *sChannel) fetchWorkBuddyCreditsBody(ctx context.Context, channel entity.Channels, credentialCipher string, input workBuddyCreditsRequest) ([]byte, error) {
	baseStatusError := input.Base.StatusError
	failures := make([]string, 0, len(workBuddyCreditsRequestVariants))
	for _, variant := range workBuddyCreditsRequestVariants {
		req := input.Base
		req.SkipClientIdentity = variant.SkipIdentity
		if variant.Apply != nil {
			headers := http.Header{}
			variant.Apply(headers)
			req.Headers = headers
		}
		variantName := variant.Name
		if baseStatusError != nil {
			// 保留调用方自己的失败处理（例如把上游原始响应写进日志），
			// 再包一层形态名，使错误信息能区分是哪一种特征被拒绝。
			req.StatusError = func(status int, payload []byte) error {
				inner := baseStatusError(status, payload)
				if inner == nil {
					inner = gerror.Newf("WorkBuddy 积分接口返回 HTTP %d", status)
				}
				return gerror.Newf("形态 %s：%s", variantName, inner.Error())
			}
		}
		body, err := s.fetchUpstreamJSON(ctx, channel, credentialCipher, req)
		if err == nil {
			g.Log().Infof(ctx, "workbuddy credits query ok: channel=%d variant=%s bytes=%d",
				channel.Id, variant.Name, len(body))
			return body, nil
		}
		failures = append(failures, fmt.Sprintf("%s → %s", variant.Name, err.Error()))
	}
	return nil, gerror.Newf("WorkBuddy 积分接口在 %d 种请求形态下均失败：%s",
		len(failures), strings.Join(failures, "；"))
}
