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

// WorkBuddy 积分查询独立于推理请求的客户端身份契约。
//
// 历史回归：0.6.27 把 chat 身份头施加给所有请求；0.6.42 又换掉了原积分 URL。
// 后续仅在新 URL 上比较请求头，不能证明旧 URL 已不可用，更不能用签到接口的
// Cookie 成功推断积分必须 Cookie。401 也不能作为路由正确的证据。
//
// 2026-10-29 官网实际积分请求仍是 POST /billing/meter/get-user-resource-summary，
// 返回 Packages、CreditGroups 和 TotalCredits；本适配器恢复原 URL，首选早期的
// 鉴权头形态，不注入 chat 专属身份，再回退同源浏览器头形态。服务端是否恢复
// 必须单独实测，官网成功和本地模拟测试都不能代替生产验收。

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
	// 优先恢复早期积分请求契约：只保留鉴权，不混入推理专属身份头。
	// 官网抓包确认积分仍使用 host 根的 summary 接口，不能拿新 URL 的 401
	// 来证明旧契约失效；浏览器形态仅作为兼容回退。
	{Name: "legacy-api", SkipIdentity: true},
	{Name: "browser-origin", SkipIdentity: true, Apply: applyWorkBuddyCreditsBrowserHeaders},
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
	return fmt.Sprintf("WorkBuddy 积分余额接口返回 HTTP %d：当前请求未通过上游鉴权。"+
		"请核对账号密钥及积分接口地址；此状态本身不能证明接口仅支持网页登录会话。"+
		"可暂到 https://www.workbuddy.cn/app 查看余额", status)
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
