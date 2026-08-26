package channel

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type CostResult struct {
	Mode            string                 `json:"mode"`
	UsedAmount      *float64               `json:"usedAmount"`
	RemainingAmount *float64               `json:"remainingAmount"`
	Currency        string                 `json:"currency"`
	Usage           *float64               `json:"usage"`
	UsageUnit       string                 `json:"usageUnit"`
	UsageType       string                 `json:"usageType"`
	UsageDimension  string                 `json:"usageDimension"`
	PeriodStart     *time.Time             `json:"periodStart"`
	PeriodEnd       *time.Time             `json:"periodEnd"`
	QueriedAt       time.Time              `json:"queriedAt"`
	Summaries       []CostSummary          `json:"summaries"`
	Credentials     []CredentialCostResult `json:"credentials"`
}

type CredentialCostResult struct {
	CredentialID    uint64    `json:"credentialId"`
	KeyPrefix       string    `json:"keyPrefix"`
	Shared          bool      `json:"shared"`
	UsedAmount      *float64  `json:"usedAmount"`
	RemainingAmount *float64  `json:"remainingAmount"`
	Currency        string    `json:"currency"`
	Usage           *float64  `json:"usage"`
	UsageUnit       string    `json:"usageUnit"`
	UsageType       string    `json:"usageType"`
	UsageDimension  string    `json:"usageDimension"`
	QueriedAt       time.Time `json:"queriedAt"`
	Error           string    `json:"error"`
}

func (s *sChannel) QueryCost(ctx context.Context, channelID uint64, input adminapi.CostQueryInput) (CostResult, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return CostResult{}, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return CostResult{}, err
	}
	start, end, err := costRange(input.StartDate, input.EndDate)
	if err != nil {
		return CostResult{}, err
	}
	if config.Costs.Adapter == channeltype.AdapterQiniuUsage {
		start, end = qiniuUsageRange(start, end)
	}
	result := CostResult{
		Mode: config.Costs.Adapter, Currency: defaultCostCurrency(config.Costs), PeriodStart: &start, PeriodEnd: &end, QueriedAt: time.Now(),
		Credentials: make([]CredentialCostResult, 0),
	}
	if config.Costs.Adapter == channeltype.AdapterNone {
		return CostResult{}, gerror.New("cost query is not configured")
	}
	if config.Costs.AuthType == channeltype.AuthChannelKey {
		// Cost queries are administrative read-only requests, not relay traffic.
		// Include disabled credentials so their remaining balance can still be inspected.
		rows := make([]credentialRow, 0)
		if err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channel.Id}).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&rows); err != nil {
			return CostResult{}, gerror.Wrap(err, "list channel credentials for cost query")
		}
		if len(rows) == 0 {
			return CostResult{}, gerror.New("channel has no upstream credential")
		}
		for _, credential := range rows {
			if err = s.ensureCredentialMetadata(ctx, &credential); err != nil {
				return CostResult{}, err
			}
			cost, queryErr := s.queryCredentialCost(ctx, channel, config.Costs, start, end, credential.ApiKeyCipher)
			detail := costCredentialResult(credential.Id, credential.KeyPrefix, false, cost, queryErr)
			result.Credentials = append(result.Credentials, detail)
			if queryErr != nil {
				continue
			}
			if saveErr := s.saveCredentialCostResult(ctx, credential.Id, cost); saveErr != nil {
				return CostResult{}, saveErr
			}
		}
		if !hasSuccessfulCostResult(result.Credentials) {
			return CostResult{}, allCredentialCostQueryError(result.Credentials)
		}
		result.applyUsageFromCredentials()
		if err = s.refreshChannelCostSummary(ctx, channel.Id, channeltype.IsUsageCost(config.Costs)); err != nil {
			return CostResult{}, err
		}
		result.Summaries, err = s.channelCostSummaries(ctx, channel.Id, channeltype.IsUsageCost(config.Costs))
		if err != nil {
			return CostResult{}, err
		}
		applyUsageSummaryMetadata(result.Summaries, config.Costs)
		result.applySingleSummary()
		if notifyErr := s.notifyChannelLowBalance(ctx, channel.Id); notifyErr != nil {
			g.Log().Warningf(ctx, "notify channel %d low balance: %v", channel.Id, notifyErr)
		}
		return result, nil
	}

	cost, queryErr := s.queryCredentialCost(ctx, channel, config.Costs, start, end, "")
	if queryErr != nil {
		return CostResult{}, queryErr
	}
	result.Credentials = append(result.Credentials, costCredentialResult(0, "管理密钥共享余额", config.Costs.AuthType == channeltype.AuthManagementKey, cost, nil))
	if err = s.saveChannelCostResult(ctx, channel.Id, cost); err != nil {
		return CostResult{}, err
	}
	result.UsedAmount, result.RemainingAmount, result.Currency = cost.UsedAmount, cost.RemainingAmount, cost.Currency
	result.Usage, result.UsageUnit, result.UsageType, result.UsageDimension = cost.Usage, cost.UsageUnit, cost.UsageType, cost.UsageDimension
	result.Summaries = []CostSummary{costSummary(cost, channeltype.IsUsageCost(config.Costs))}
	applyUsageSummaryMetadata(result.Summaries, config.Costs)
	if notifyErr := s.notifyChannelLowBalance(ctx, channel.Id); notifyErr != nil {
		g.Log().Warningf(ctx, "notify channel %d low balance: %v", channel.Id, notifyErr)
	}
	return result, nil
}

func defaultCostCurrency(config channeltype.CostConfig) string {
	if config.FixedCurrency != "" {
		return strings.ToUpper(config.FixedCurrency)
	}
	return "USD"
}

func (s *sChannel) queryCredentialCost(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, start, end time.Time, credentialCipher string) (CostResult, error) {
	result := CostResult{Mode: config.Adapter, Currency: defaultCostCurrency(config), PeriodStart: &start, PeriodEnd: &end, QueriedAt: time.Now()}
	var err error
	switch config.Adapter {
	case channeltype.AdapterOpenAICosts:
		err = s.queryOpenAICosts(ctx, channel, credentialCipher, config, start, end, &result)
	case channeltype.AdapterSub2API:
		err = s.querySub2API(ctx, channel, credentialCipher, config, &result)
	case channeltype.AdapterNewAPI:
		err = s.queryNewAPI(ctx, channel, config, &result)
	case channeltype.AdapterCustomJSON:
		err = s.queryCustomJSON(ctx, channel, credentialCipher, config, &result)
	case channeltype.AdapterSiliconFlow:
		err = s.querySiliconFlowBalance(ctx, channel, credentialCipher, config, &result)
	case channeltype.AdapterOpenRouter:
		err = s.queryOpenRouterCredits(ctx, channel, credentialCipher, config, &result)
	case channeltype.AdapterQiniuCosts:
		err = s.queryQiniuCosts(ctx, channel, credentialCipher, config, &result)
	case channeltype.AdapterQiniuUsage:
		err = s.queryQiniuUsage(ctx, channel, credentialCipher, config, start, end, &result)
	default:
		err = gerror.New("cost query is not configured")
	}
	return result, err
}

func costCredentialResult(credentialID uint64, keyPrefix string, shared bool, cost CostResult, err error) CredentialCostResult {
	detail := CredentialCostResult{CredentialID: credentialID, KeyPrefix: keyPrefix, Shared: shared, Currency: cost.Currency, QueriedAt: cost.QueriedAt}
	if err != nil {
		detail.Error = err.Error()
		return detail
	}
	detail.UsedAmount = cost.UsedAmount
	detail.RemainingAmount = cost.RemainingAmount
	detail.Usage = cost.Usage
	detail.UsageUnit = cost.UsageUnit
	detail.UsageType = cost.UsageType
	detail.UsageDimension = cost.UsageDimension
	return detail
}

func hasSuccessfulCostResult(results []CredentialCostResult) bool {
	for _, item := range results {
		if item.Error == "" {
			return true
		}
	}
	return false
}

func allCredentialCostQueryError(results []CredentialCostResult) error {
	details := make([]string, 0, len(results))
	for _, item := range results {
		if item.Error == "" {
			continue
		}
		keyPrefix := strings.TrimSpace(item.KeyPrefix)
		if keyPrefix == "" {
			keyPrefix = "未标识密钥"
		}
		details = append(details, keyPrefix+"："+localizedCostQueryError(item.Error))
	}
	if len(details) == 0 {
		return gerror.New("所有上游密钥的费用/余额查询均失败，未返回可用的上游错误详情")
	}
	return gerror.New("所有上游密钥的费用/余额查询均失败：" + strings.Join(details, "；"))
}

func localizedCostQueryError(message string) string {
	message = strings.TrimSpace(message)
	switch {
	case strings.Contains(message, "HTTP 401"):
		return "上游接口返回 HTTP 401，密钥无效、已过期或没有余额查询权限"
	case strings.Contains(message, "HTTP 403"):
		return "上游接口返回 HTTP 403，当前密钥没有余额查询权限"
	case strings.Contains(message, "HTTP 404"):
		return "上游接口返回 HTTP 404，渠道配置的余额查询地址不存在"
	case strings.Contains(message, "HTTP 429"):
		return "上游接口返回 HTTP 429，请求过于频繁或账户已触发限流"
	case strings.Contains(message, "HTTP 5"):
		return "上游服务异常，请稍后重试"
	case strings.Contains(message, "context deadline exceeded"):
		return "请求上游接口超时，请检查渠道网络、代理或上游服务状态"
	case strings.Contains(message, "请求上游费用/余额接口失败"):
		return "无法连接上游费用/余额接口，请检查渠道网络、代理或上游服务状态"
	case strings.Contains(message, "上游费用/余额接口返回了无效 JSON"):
		return "上游接口返回格式异常，无法解析费用或余额"
	default:
		return "上游费用/余额查询失败：" + message
	}
}

func (r *CostResult) applyUsageFromCredentials() {
	for _, item := range r.Credentials {
		if item.Error == "" && item.Usage != nil {
			if r.Usage == nil {
				value := 0.0
				r.Usage = &value
			}
			*r.Usage += *item.Usage
			r.UsageUnit = item.UsageUnit
			r.UsageType = item.UsageType
			r.UsageDimension = item.UsageDimension
		}
	}
}

func (r *CostResult) applySingleSummary() {
	if len(r.Summaries) != 1 {
		r.UsedAmount = nil
		r.RemainingAmount = nil
		r.Currency = ""
		return
	}
	r.UsedAmount = r.Summaries[0].UsedAmount
	r.RemainingAmount = r.Summaries[0].RemainingAmount
	r.Currency = r.Summaries[0].Currency
	r.Usage = r.Summaries[0].Usage
	r.UsageUnit = r.Summaries[0].UsageUnit
	r.UsageType = r.Summaries[0].UsageType
	r.UsageDimension = r.Summaries[0].UsageDimension
}

func costSummary(cost CostResult, usage bool) CostSummary {
	summary := CostSummary{Currency: cost.Currency, UsedAmount: cost.UsedAmount, RemainingAmount: cost.RemainingAmount}
	if usage {
		summary.Usage = cost.Usage
		summary.UsageUnit = cost.UsageUnit
		summary.UsageType = cost.UsageType
		summary.UsageDimension = cost.UsageDimension
	}
	return summary
}

func applyUsageSummaryMetadata(summaries []CostSummary, config channeltype.CostConfig) {
	if !channeltype.IsUsageCost(config) {
		return
	}
	unit := config.UsageUnit
	if unit == "" {
		unit = "kToken"
	}
	usageType := config.UsageType
	if usageType == "" {
		usageType = "用量"
	}
	for index := range summaries {
		summaries[index].UsageUnit = unit
		summaries[index].UsageType = usageType
		summaries[index].UsageDimension = config.UsageDimension
	}
}
