package channel

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

type CostSummary struct {
	Currency        string   `json:"currency"`
	UsedAmount      *float64 `json:"usedAmount"`
	RemainingAmount *float64 `json:"remainingAmount"`
}

type credentialCostState struct {
	Used      *float64 `orm:"last_cost_used"`
	Remaining *float64 `orm:"last_cost_remaining"`
	Currency  string   `orm:"last_cost_currency"`
}

type channelCostState struct {
	Used      *float64   `orm:"last_cost_used"`
	Remaining *float64   `orm:"last_cost_remaining"`
	Currency  string     `orm:"last_cost_currency"`
	At        *time.Time `orm:"last_cost_at"`
}

type trackedChannelCost struct {
	Name      string   `orm:"name"`
	Remaining *float64 `orm:"last_cost_remaining"`
}

func (s *Service) currentChannelCost(ctx context.Context, channelID uint64) (channelCostState, error) {
	var state channelCostState
	err := dao.Channels.Ctx(ctx).
		Fields("last_cost_used,last_cost_remaining,last_cost_currency,last_cost_at").
		Where(dao.Channels.Columns().Id, channelID).
		Scan(&state)
	return state, gerror.Wrap(err, "load current channel cost")
}

// ApplyUsageCost preserves the legacy channel-level accounting entry point for
// callers that do not know the selected credential. Relay requests use the
// credential-aware method below.
func (s *Service) ApplyUsageCost(ctx context.Context, channelID uint64, amount decimal.Decimal) error {
	amount = amount.Round(8)
	if channelID == 0 || amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	literal := amount.StringFixed(8)
	columns := dao.Channels.Columns()
	_, err := dao.Channels.Ctx(ctx).
		Where(columns.Id, channelID).
		Where("(last_cost_currency IS NULL OR last_cost_currency = '' OR UPPER(last_cost_currency) = 'USD')").
		Data(do.Channels{
			LastCostUsed:      gdb.Raw("COALESCE(last_cost_used, 0) + " + literal),
			LastCostRemaining: gdb.Raw("CASE WHEN last_cost_remaining IS NULL THEN NULL ELSE GREATEST(last_cost_remaining - " + literal + ", 0) END"),
			LastCostCurrency:  gdb.Raw("COALESCE(NULLIF(last_cost_currency, ''), 'USD')"),
			LastCostAt:        time.Now(),
		}).Update()
	return gerror.Wrap(err, "apply legacy channel usage cost")
}

func (s *Service) ApplyCredentialUsageCost(ctx context.Context, channelID, credentialID uint64, amount decimal.Decimal) error {
	amount = amount.Round(8)
	if channelID == 0 || credentialID == 0 || amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	literal := amount.StringFixed(8)
	columns := dao.ChannelCredentials.Columns()
	result, err := dao.ChannelCredentials.Ctx(ctx).
		Where(columns.Id, credentialID).
		Where(columns.ChannelId, channelID).
		Where("(last_cost_currency IS NULL OR last_cost_currency = '' OR UPPER(last_cost_currency) = 'USD')").
		Data(do.ChannelCredentials{
			LastCostUsed:      gdb.Raw("COALESCE(last_cost_used, 0) + " + literal),
			LastCostRemaining: gdb.Raw("CASE WHEN last_cost_remaining IS NULL THEN NULL ELSE GREATEST(last_cost_remaining - " + literal + ", 0) END"),
			LastCostCurrency:  gdb.Raw("COALESCE(NULLIF(last_cost_currency, ''), 'USD')"),
			LastCostAt:        time.Now(),
		}).Update()
	if err != nil {
		return gerror.Wrap(err, "apply channel credential usage cost")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil
	}
	if err = s.refreshChannelCostSummary(ctx, channelID); err != nil {
		return err
	}
	var state trackedChannelCost
	if err = dao.Channels.Ctx(ctx).
		Fields(dao.Channels.Columns().Name, dao.Channels.Columns().LastCostRemaining).
		Where(dao.Channels.Columns().Id, channelID).
		Scan(&state); err != nil {
		return gerror.Wrap(err, "load tracked channel cost")
	}
	if state.Remaining != nil && s.mail != nil {
		s.mail.NotifyChannelLowBalance(ctx, channelID, state.Name, *state.Remaining)
	}
	return nil
}

func (s *Service) channelCostSummaries(ctx context.Context, channelID uint64) ([]CostSummary, error) {
	states := make([]credentialCostState, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).
		Fields("last_cost_used,last_cost_remaining,last_cost_currency").
		Where(do.ChannelCredentials{ChannelId: channelID}).
		Scan(&states); err != nil {
		return nil, gerror.Wrap(err, "load channel credential cost summaries")
	}
	totals := make(map[string]CostSummary)
	for _, state := range states {
		currency := strings.ToUpper(strings.TrimSpace(state.Currency))
		if currency == "" {
			continue
		}
		total := totals[currency]
		total.Currency = currency
		if state.Used != nil {
			value := *state.Used
			if total.UsedAmount != nil {
				value += *total.UsedAmount
			}
			total.UsedAmount = &value
		}
		if state.Remaining != nil {
			value := *state.Remaining
			if total.RemainingAmount != nil {
				value += *total.RemainingAmount
			}
			total.RemainingAmount = &value
		}
		totals[currency] = total
	}
	result := make([]CostSummary, 0, len(totals))
	for _, summary := range totals {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Currency < result[j].Currency })
	return result, nil
}

func (s *Service) refreshChannelCostSummary(ctx context.Context, channelID uint64) error {
	summaries, err := s.channelCostSummaries(ctx, channelID)
	if err != nil {
		return err
	}
	data := do.Channels{LastCostAt: time.Now()}
	if len(summaries) == 1 {
		data.LastCostCurrency = summaries[0].Currency
		data.LastCostUsed = nullableNumber(summaries[0].UsedAmount)
		data.LastCostRemaining = nullableNumber(summaries[0].RemainingAmount)
	} else {
		data.LastCostCurrency = gdb.Raw("NULL")
		data.LastCostUsed = gdb.Raw("NULL")
		data.LastCostRemaining = gdb.Raw("NULL")
	}
	if _, err = dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, channelID).Data(data).Update(); err != nil {
		return gerror.Wrap(err, "refresh channel cost summary")
	}
	return nil
}
