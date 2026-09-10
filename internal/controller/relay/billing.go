package relay

import (
	"net/http"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/logic/apikey"
	relaysvc "github.com/yunloli/aiferry/internal/logic/relay"
)

// billingSubscription 返回 OpenAI 兼容的密钥余额视图：
// hard_limit_usd = min(用户总余额, 密钥剩余限额)。
func (c *Controller) billingSubscription(r *ghttp.Request) {
	c.withAuthenticatedKey(r, func(key apikey.AuthKey) {
		view, err := c.relay.Subscription(r.Context(), key)
		writeBillingJSON(r, view, err)
	})
}

// billingUsage 返回该密钥在时间窗口内的用量，OpenAI 兼容格式。
// 支持 start_date / end_date（YYYY-MM-DD），end_date 缺省为今天，start_date 缺省为 30 天前。
func (c *Controller) billingUsage(r *ghttp.Request) {
	c.withAuthenticatedKey(r, func(key apikey.AuthKey) {
		now := time.Now()
		end := parseBillingDate(r.Get("end_date").String(), now)
		start := parseBillingDate(r.Get("start_date").String(), now.AddDate(0, 0, -30))
		view, err := c.relay.Usage(r.Context(), key, start, end)
		writeBillingJSON(r, view, err)
	})
}

// billingChannels 返回各渠道最新余额快照，仅管理员角色的密钥可用。
func (c *Controller) billingChannels(r *ghttp.Request) {
	c.withAuthenticatedKey(r, func(key apikey.AuthKey) {
		view, err := c.relay.ChannelsBalance(r.Context(), key)
		writeBillingJSON(r, view, err)
	})
}

func writeBillingJSON(r *ghttp.Request, view any, err error) {
	if err != nil {
		if relaysvc.IsBillingPermissionError(err) {
			writeError(r, http.StatusForbidden, "insufficient_quota", err.Error())
			return
		}
		if relaysvc.IsBillingRequestError(err) {
			writeError(r, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		writeError(r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	r.Response.Header().Set("Content-Type", "application/json")
	r.Response.WriteJson(view)
	r.Exit()
}

// parseBillingDate 解析 YYYY-MM-DD；空值或非法格式返回 fallback。
func parseBillingDate(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return fallback
	}
	return parsed
}
