package channel

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

type CostSummary struct {
	Currency        string   `json:"currency"`
	UsedAmount      *float64 `json:"usedAmount"`
	RemainingAmount *float64 `json:"remainingAmount"`
	Usage           *float64 `json:"usage,omitempty"`
	UsageUnit       string   `json:"usageUnit,omitempty"`
	UsageType       string   `json:"usageType,omitempty"`
	UsageDimension  string   `json:"usageDimension,omitempty"`
}

type credentialCostState struct {
	Id        uint64   `orm:"id"`
	Used      *float64 `orm:"last_cost_used"`
	Remaining *float64 `orm:"last_cost_remaining"`
	Currency  string   `orm:"last_cost_currency"`
}

type channelCostState struct {
	Id        uint64     `orm:"id"`
	Used      *float64   `orm:"last_cost_used"`
	Remaining *float64   `orm:"last_cost_remaining"`
	Currency  string     `orm:"last_cost_currency"`
	At        *time.Time `orm:"last_cost_at"`
}

type trackedChannelCost struct {
	Name      string   `orm:"name"`
	Remaining *float64 `orm:"last_cost_remaining"`
	Currency  string   `orm:"last_cost_currency"`
}

func (s *sChannel) currentChannelCost(ctx context.Context, channelID uint64) (channelCostState, error) {
	var state channelCostState
	columns := dao.Channels.Columns()
	err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.LastCostUsed, columns.LastCostRemaining, columns.LastCostCurrency, columns.LastCostAt).
		Where(columns.Id, channelID).
		Scan(&state)
	return state, gerror.Wrap(err, "load current channel cost")
}

// ApplyUsageCost preserves the legacy channel-level accounting entry point for
// callers that do not know the selected credential. Relay requests use the
// credential-aware method below.
func (s *sChannel) ApplyUsageCost(ctx context.Context, channelID uint64, amount decimal.Decimal) error {
	amount = amount.Round(8)
	if channelID == 0 || amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	return dao.Channels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		columns := dao.Channels.Columns()
		var state channelCostState
		if err := dao.Channels.Ctx(txCtx).
			Fields(columns.Id, columns.LastCostUsed, columns.LastCostRemaining, columns.LastCostCurrency).
			Where(columns.Id, channelID).
			Lock(gdb.LockForUpdate).
			Scan(&state); err != nil {
			return gerror.Wrap(err, "lock legacy channel cost")
		}
		if state.Id == 0 || !tracksUSDCost(state.Currency) {
			return nil
		}
		used, remaining, currency := applyTrackedCost(state.Used, state.Remaining, state.Currency, amount)
		if _, err := dao.Channels.Ctx(txCtx).Where(columns.Id, state.Id).Data(do.Channels{
			LastCostUsed:      used,
			LastCostRemaining: remaining,
			LastCostCurrency:  currency,
			LastCostAt:        time.Now(),
		}).Update(); err != nil {
			return gerror.Wrap(err, "apply legacy channel usage cost")
		}
		return nil
	})
}

func (s *sChannel) ApplyCredentialUsageCost(ctx context.Context, channelID, credentialID uint64, amount decimal.Decimal) error {
	amount = amount.Round(8)
	if channelID == 0 || credentialID == 0 || amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	updated := false
	err := dao.ChannelCredentials.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		columns := dao.ChannelCredentials.Columns()
		var state credentialCostState
		if err := dao.ChannelCredentials.Ctx(txCtx).
			Fields(columns.Id, columns.LastCostUsed, columns.LastCostRemaining, columns.LastCostCurrency).
			Where(columns.Id, credentialID).
			Where(columns.ChannelId, channelID).
			Lock(gdb.LockForUpdate).
			Scan(&state); err != nil {
			return gerror.Wrap(err, "lock channel credential cost")
		}
		if state.Id == 0 || !tracksUSDCost(state.Currency) {
			return nil
		}
		used, remaining, currency := applyTrackedCost(state.Used, state.Remaining, state.Currency, amount)
		if _, err := dao.ChannelCredentials.Ctx(txCtx).Where(columns.Id, state.Id).Data(do.ChannelCredentials{
			LastCostUsed:      used,
			LastCostRemaining: remaining,
			LastCostCurrency:  currency,
			LastCostAt:        time.Now(),
		}).Update(); err != nil {
			return gerror.Wrap(err, "apply channel credential usage cost")
		}
		updated = true
		return nil
	})
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	if err = s.refreshChannelCostSummary(ctx, channelID); err != nil {
		return err
	}
	if err = s.notifyChannelLowBalance(ctx, channelID); err != nil {
		g.Log().Warningf(ctx, "notify channel %d low balance: %v", channelID, err)
	}
	return nil
}

func (s *sChannel) notifyChannelLowBalance(ctx context.Context, channelID uint64) error {
	if s.mail == nil {
		return nil
	}
	var state trackedChannelCost
	if err := dao.Channels.Ctx(ctx).
		Fields(dao.Channels.Columns().Name, dao.Channels.Columns().LastCostRemaining, dao.Channels.Columns().LastCostCurrency).
		Where(dao.Channels.Columns().Id, channelID).
		Scan(&state); err != nil {
		return gerror.Wrap(err, "load tracked channel cost")
	}
	if state.Remaining != nil {
		s.mail.NotifyChannelLowBalance(ctx, channelID, state.Name, *state.Remaining, state.Currency)
	}
	return nil
}

func (s *sChannel) channelCostSummaries(ctx context.Context, channelID uint64, usage ...bool) ([]CostSummary, error) {
	states := make([]credentialCostState, 0)
	columns := dao.ChannelCredentials.Columns()
	if err := dao.ChannelCredentials.Ctx(ctx).
		Fields(columns.LastCostUsed, columns.LastCostRemaining, columns.LastCostCurrency).
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
		if len(usage) > 0 && usage[0] {
			summary.Usage = summary.UsedAmount
			summary.UsageUnit = "kToken"
			summary.UsageType = "用量"
		}
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Currency < result[j].Currency })
	return result, nil
}

func tracksUSDCost(currency string) bool {
	return currency == "" || strings.EqualFold(currency, "USD")
}

func applyTrackedCost(used, remaining *float64, currency string, amount decimal.Decimal) (float64, *float64, string) {
	amountValue, _ := amount.Float64()
	updatedUsed := amountValue
	if used != nil {
		updatedUsed += *used
	}
	if remaining != nil {
		updatedRemaining := *remaining - amountValue
		if updatedRemaining < 0 {
			updatedRemaining = 0
		}
		remaining = &updatedRemaining
	}
	if currency == "" {
		currency = "USD"
	}
	return updatedUsed, remaining, currency
}

func (s *sChannel) refreshChannelCostSummary(ctx context.Context, channelID uint64, usage ...bool) error {
	summaries, err := s.channelCostSummaries(ctx, channelID, usage...)
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
