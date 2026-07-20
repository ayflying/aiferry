package channel

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

type CostResult struct {
	Mode            string                 `json:"mode"`
	UsedAmount      *float64               `json:"usedAmount"`
	RemainingAmount *float64               `json:"remainingAmount"`
	Currency        string                 `json:"currency"`
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
	QueriedAt       time.Time `json:"queriedAt"`
	Error           string    `json:"error"`
}

func (s *Service) QueryCost(ctx context.Context, channelID uint64, input adminapi.CostQueryInput) (CostResult, error) {
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
			return CostResult{}, gerror.New("all upstream credential cost queries failed")
		}
		if err = s.refreshChannelCostSummary(ctx, channel.Id); err != nil {
			return CostResult{}, err
		}
		result.Summaries, err = s.channelCostSummaries(ctx, channel.Id)
		if err != nil {
			return CostResult{}, err
		}
		result.applySingleSummary()
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
	result.Summaries = []CostSummary{{Currency: cost.Currency, UsedAmount: cost.UsedAmount, RemainingAmount: cost.RemainingAmount}}
	return result, nil
}

func defaultCostCurrency(config channeltype.CostConfig) string {
	if config.FixedCurrency != "" {
		return strings.ToUpper(config.FixedCurrency)
	}
	return "USD"
}

func (s *Service) queryCredentialCost(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, start, end time.Time, credentialCipher string) (CostResult, error) {
	result := CostResult{Mode: config.Adapter, Currency: defaultCostCurrency(config), PeriodStart: &start, PeriodEnd: &end, QueriedAt: time.Now()}
	var err error
	switch config.Adapter {
	case channeltype.AdapterOpenAICosts:
		err = s.queryOpenAICosts(ctx, channel, credentialCipher, config, start, end, &result)
	case channeltype.AdapterSub2API:
		err = s.querySub2API(ctx, channel, credentialCipher, config, &result)
	case channeltype.AdapterCustomJSON:
		err = s.queryCustomJSON(ctx, channel, credentialCipher, config, &result)
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
}
