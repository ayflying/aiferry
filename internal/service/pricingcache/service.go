package pricingcache

import (
	"context"
	"sort"
	"sync/atomic"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/usage"
)

const (
	billingModeToken   = "token"
	billingModeRequest = "request"
	billingModeRules   = "rules"
)

type Service struct {
	snapshot atomic.Value
}

type modelPrice struct {
	BillingMode string
	Rates       usage.PriceRates
	Rules       []priceRule
}

type priceRule struct {
	Name       string
	Conditions string
	Rates      string
	Priority   int
	Source     string
	Currency   string
	ID         uint64
}

type snapshot map[string]modelPrice

type priceRow struct {
	PublicName       string   `orm:"public_name"`
	BillingMode      string   `orm:"billing_mode"`
	InputPrice       *float64 `orm:"input_price"`
	CachedInputPrice *float64 `orm:"cached_input_price"`
	CacheWritePrice  *float64 `orm:"cache_write_price"`
	OutputPrice      *float64 `orm:"output_price"`
	ImageInputPrice  *float64 `orm:"image_input_price"`
	AudioInputPrice  *float64 `orm:"audio_input_price"`
	AudioOutputPrice *float64 `orm:"audio_output_price"`
	RequestPrice     *float64 `orm:"request_price"`
}

type ruleRow struct {
	ModelName      string `orm:"model_name"`
	Name           string `orm:"name"`
	ConditionsJSON string `orm:"conditions_json"`
	RatesJSON      string `orm:"rates_json"`
	Priority       int    `orm:"priority"`
	Source         string `orm:"source"`
	Currency       string `orm:"currency"`
	ID             uint64 `orm:"id"`
}

func New() *Service {
	service := &Service{}
	service.snapshot.Store(snapshot{})
	return service
}

func (s *Service) Load(ctx context.Context) error {
	prices := make([]priceRow, 0)
	if err := dao.ModelPrices.Ctx(ctx).Scan(&prices); err != nil {
		return gerror.Wrap(err, "load model price cache")
	}
	rules := make([]ruleRow, 0)
	if err := dao.ModelPriceRules.Ctx(ctx).
		Where(dao.ModelPriceRules.Columns().Status, 1).
		Scan(&rules); err != nil {
		return gerror.Wrap(err, "load model price rule cache")
	}

	loaded := make(snapshot, len(prices))
	for _, price := range prices {
		loaded[price.PublicName] = modelPrice{
			BillingMode: normalizeBillingMode(price.BillingMode),
			Rates: usage.PriceRates{
				Input: price.InputPrice, CachedInput: price.CachedInputPrice, CacheWrite: price.CacheWritePrice,
				Output: price.OutputPrice, ImageInput: price.ImageInputPrice, AudioInput: price.AudioInputPrice,
				AudioOutput: price.AudioOutputPrice, Request: price.RequestPrice,
			},
		}
	}
	for _, rule := range rules {
		price, exists := loaded[rule.ModelName]
		if !exists {
			price = modelPrice{BillingMode: billingModeRules}
		}
		price.Rules = append(price.Rules, priceRule{
			Name: rule.Name, Conditions: rule.ConditionsJSON, Rates: rule.RatesJSON, Priority: rule.Priority,
			Source: rule.Source, Currency: rule.Currency, ID: rule.ID,
		})
		loaded[rule.ModelName] = price
	}
	for name, price := range loaded {
		sort.SliceStable(price.Rules, func(i, j int) bool {
			if price.Rules[i].Priority != price.Rules[j].Priority {
				return price.Rules[i].Priority > price.Rules[j].Priority
			}
			if price.Rules[i].Source != price.Rules[j].Source {
				return price.Rules[i].Source == "manual"
			}
			return price.Rules[i].ID > price.Rules[j].ID
		})
		loaded[name] = price
	}
	s.snapshot.Store(loaded)
	return nil
}

func (s *Service) IsPriced(modelName string) bool {
	price, exists := s.current()[modelName]
	if !exists {
		return false
	}
	switch price.BillingMode {
	case billingModeRules:
		return len(price.Rules) > 0
	case billingModeRequest:
		return price.Rates.Request != nil
	default:
		return price.Rates.Input != nil || price.Rates.CachedInput != nil || price.Rates.CacheWrite != nil ||
			price.Rates.Output != nil || price.Rates.ImageInput != nil || price.Rates.AudioInput != nil || price.Rates.AudioOutput != nil
	}
}

func (s *Service) EstimateBreakdown(modelName, endpoint string, tokens usage.TokenUsage) *usage.BillingBreakdown {
	price, exists := s.current()[modelName]
	if !exists {
		return nil
	}
	switch price.BillingMode {
	case billingModeRules:
		for _, rule := range price.Rules {
			if breakdown, matches := usage.RuleBreakdown(rule.Conditions, rule.Rates, endpoint, tokens); matches {
				breakdown.BillingMode = billingModeRules
				breakdown.Currency = normalizeCurrency(rule.Currency)
				breakdown.Rule = &usage.BillingRuleSnapshot{
					ID: rule.ID, Name: rule.Name, Source: rule.Source, Priority: rule.Priority, Conditions: rule.Conditions,
				}
				return breakdown
			}
		}
		return nil
	case billingModeRequest:
		breakdown := usage.EstimateBreakdown(tokens, usage.PriceRates{Request: price.Rates.Request})
		if breakdown != nil {
			breakdown.BillingMode = billingModeRequest
		}
		return breakdown
	default:
		return usage.EstimateBreakdown(tokens, price.Rates)
	}
}

func (s *Service) Estimate(modelName, endpoint string, tokens usage.TokenUsage) *decimal.Decimal {
	breakdown := s.EstimateBreakdown(modelName, endpoint, tokens)
	if breakdown == nil {
		return nil
	}
	cost := breakdown.Cost()
	return &cost
}

func (s *Service) current() snapshot {
	return s.snapshot.Load().(snapshot)
}

func normalizeBillingMode(value string) string {
	switch value {
	case billingModeRequest, billingModeRules:
		return value
	default:
		return billingModeToken
	}
}

func normalizeCurrency(value string) string {
	if value == "" {
		return "USD"
	}
	return value
}
