package channel

import (
	"context"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

func (s *sChannel) queryNewAPI(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, result *CostResult) error {
	accountEndpoint, err := newAPIEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	account, err := s.getCostJSON(ctx, channel, "", accountEndpoint, config)
	if err != nil {
		return err
	}
	statusEndpoint, err := newAPIEndpointURL(channel.BaseUrl, "/api/status")
	if err != nil {
		return err
	}
	statusConfig := config
	statusConfig.AuthType = channeltype.AuthNone
	statusConfig.HeaderName = ""
	statusConfig.HeaderPrefix = ""
	status, err := s.getCostJSON(ctx, channel, "", statusEndpoint, statusConfig)
	if err != nil {
		return err
	}
	used, remaining, quotaPerUnit, err := newAPICostAmounts(account, status)
	if err != nil {
		return err
	}
	// 订阅套餐额度并入合计：NewAPI 除钱包外还有订阅制套餐（按订阅扣费），
	// 只看钱包会低估真实可用额度。/api/subscription/self 与 /api/user/self
	// 同为 UserAuth，管理密钥通用；旧版部署没有该接口，失败时静默降级为纯钱包。
	subEndpoint, subErr := newAPIEndpointURL(channel.BaseUrl, "/api/subscription/self")
	if subErr == nil {
		if subBody, queryErr := s.getCostJSON(ctx, channel, "", subEndpoint, config); queryErr == nil {
			subUsed, subRemaining := newAPISubscriptionAmounts(subBody)
			*used += subUsed / quotaPerUnit
			*remaining += subRemaining / quotaPerUnit
		}
	}
	result.UsedAmount = used
	result.RemainingAmount = remaining
	return nil
}

func newAPIEndpointURL(baseURL, endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", gerror.New("NewAPI channel base URL must be an absolute HTTP(S) URL ending in /v1")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		return "", gerror.New("NewAPI channel base URL must end in /v1")
	}
	parsed.Path = strings.TrimSuffix(path, "/v1") + "/" + strings.TrimLeft(endpoint, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func newAPICostAmounts(account, status []byte) (*float64, *float64, float64, error) {
	if !newAPIResponseSucceeded(account) {
		return nil, nil, 0, gerror.New("NewAPI account query was not successful")
	}
	if !newAPIResponseSucceeded(status) {
		return nil, nil, 0, gerror.New("NewAPI status query was not successful")
	}
	usedQuota := jsonFloat(account, "data.used_quota")
	remainingQuota := jsonFloat(account, "data.quota")
	if usedQuota == nil || remainingQuota == nil {
		return nil, nil, 0, gerror.New("NewAPI account response did not contain quota values")
	}
	quotaPerUnit := gjson.GetBytes(status, "data.quota_per_unit").Float()
	if quotaPerUnit <= 0 {
		return nil, nil, 0, gerror.New("NewAPI status response did not contain a valid quota_per_unit")
	}
	used := *usedQuota / quotaPerUnit
	remaining := *remainingQuota / quotaPerUnit
	return &used, &remaining, quotaPerUnit, nil
}

// newAPISubscriptionAmounts 汇总活跃订阅的已用与剩余额度（原始 quota 刻度）。
// 响应结构：data.subscriptions[].subscription.{amount_total, amount_used}；
// amount_total<=0 表示不限量订阅，不计入剩余（避免虚增），仅累计已用。
// 接口不存在或结构变化时返回 0，调用方降级为纯钱包余额。
func newAPISubscriptionAmounts(body []byte) (used, remaining float64) {
	if !newAPIResponseSucceeded(body) {
		return 0, 0
	}
	subscriptions := gjson.GetBytes(body, "data.subscriptions").Array()
	for _, item := range subscriptions {
		subscription := item.Get("subscription")
		if !subscription.Exists() {
			continue
		}
		total := subscription.Get("amount_total").Float()
		usedTotal := subscription.Get("amount_used").Float()
		used += usedTotal
		if total > 0 {
			remaining += total - usedTotal
		}
	}
	return used, remaining
}

func newAPIResponseSucceeded(body []byte) bool {
	value := gjson.GetBytes(body, "success")
	return !value.Exists() || value.Bool()
}
