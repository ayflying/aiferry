package usage

import (
	"encoding/json"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	billingUnitPerMillionTokens = "per_million_tokens"
	billingUnitPerRequest       = "per_request"
	billingUnitSettlement       = "settlement"
)

type BillingItem struct {
	Type        string `json:"type"`
	Quantity    uint64 `json:"quantity"`
	Unit        string `json:"unit"`
	UnitPrice   string `json:"unitPrice"`
	PriceSource string `json:"priceSource,omitempty"`
	Amount      string `json:"amount"`
}

type BillingRuleSnapshot struct {
	ID         uint64 `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Priority   int    `json:"priority"`
	Conditions string `json:"conditions"`
}

type BillingBreakdown struct {
	BillingMode   string               `json:"billingMode"`
	Currency      string               `json:"currency"`
	Charged       bool                 `json:"charged"`
	Reconstructed bool                 `json:"reconstructed,omitempty"`
	Rule          *BillingRuleSnapshot `json:"rule,omitempty"`
	Items         []BillingItem        `json:"items"`
	Subtotal      string               `json:"subtotal"`
	Total         string               `json:"total"`
}

func EstimateBreakdown(tokens TokenUsage, rates PriceRates) *BillingBreakdown {
	breakdown := &BillingBreakdown{
		BillingMode: "token",
		Currency:    "USD",
		Items:       make([]BillingItem, 0, 8),
	}
	var (
		cost            = decimal.Zero
		priced          bool
		inputRemaining  = remainingTokens(tokens.Input)
		outputRemaining = remainingTokens(tokens.Output)
	)

	chargeInput := func(kind string, value *uint64, preferred, fallback *float64) {
		quantity := boundedTokenCount(value, inputRemaining)
		if inputRemaining != nil {
			*inputRemaining -= quantity
		}
		price, source := preferredPrice(kind, preferred, fallback, "input")
		if price == nil || quantity == 0 {
			return
		}
		cost = cost.Add(addTokenItem(&breakdown.Items, kind, quantity, *price, source))
		priced = true
	}
	chargeOutput := func(kind string, value *uint64, preferred, fallback *float64) {
		quantity := boundedTokenCount(value, outputRemaining)
		if outputRemaining != nil {
			*outputRemaining -= quantity
		}
		price, source := preferredPrice(kind, preferred, fallback, "output")
		if price == nil || quantity == 0 {
			return
		}
		cost = cost.Add(addTokenItem(&breakdown.Items, kind, quantity, *price, source))
		priced = true
	}

	chargeInput("cached_input", tokens.CachedInput, rates.CachedInput, rates.Input)
	chargeInput("cache_write", tokens.CacheWrite, rates.CacheWrite, rates.Input)
	chargeInput("image_input", tokens.ImageInput, rates.ImageInput, rates.Input)
	chargeInput("audio_input", tokens.AudioInput, rates.AudioInput, rates.Input)
	if inputRemaining != nil && rates.Input != nil && *inputRemaining > 0 {
		cost = cost.Add(addTokenItem(&breakdown.Items, "input", *inputRemaining, *rates.Input, "input"))
		priced = true
	}
	chargeOutput("audio_output", tokens.AudioOutput, rates.AudioOutput, rates.Output)
	if outputRemaining != nil && rates.Output != nil && *outputRemaining > 0 {
		cost = cost.Add(addTokenItem(&breakdown.Items, "output", *outputRemaining, *rates.Output, "output"))
		priced = true
	}
	if rates.Request != nil {
		amount := decimal.NewFromFloat(*rates.Request)
		breakdown.Items = append(breakdown.Items, BillingItem{
			Type: "request", Quantity: 1, Unit: billingUnitPerRequest,
			UnitPrice: amount.String(), PriceSource: "request", Amount: amount.String(),
		})
		cost = cost.Add(amount)
		priced = true
	}
	if !priced {
		return nil
	}

	breakdown.Subtotal = cost.String()
	settled := cost.Round(8)
	if adjustment := settled.Sub(cost); !adjustment.IsZero() {
		breakdown.Items = append(breakdown.Items, BillingItem{
			Type: "rounding", Quantity: 1, Unit: billingUnitSettlement,
			UnitPrice: adjustment.String(), PriceSource: "settlement", Amount: adjustment.String(),
		})
	}
	breakdown.Total = settled.StringFixed(8)
	return breakdown
}

func EstimateCost(tokens TokenUsage, rates PriceRates) *decimal.Decimal {
	breakdown := EstimateBreakdown(tokens, rates)
	if breakdown == nil {
		return nil
	}
	cost := breakdown.Cost()
	return &cost
}

func (b *BillingBreakdown) Cost() decimal.Decimal {
	if b == nil || strings.TrimSpace(b.Total) == "" {
		return decimal.Zero
	}
	return decimal.RequireFromString(b.Total)
}

func (b *BillingBreakdown) JSON() (string, error) {
	encoded, err := json.Marshal(b)
	return string(encoded), err
}

func ParseBillingBreakdown(raw string) *BillingBreakdown {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var result BillingBreakdown
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result.Total == "" {
		return nil
	}
	return &result
}

func addTokenItem(items *[]BillingItem, kind string, quantity uint64, rate float64, source string) decimal.Decimal {
	price := decimal.NewFromFloat(rate)
	amount := decimal.NewFromInt(int64(quantity)).Mul(price).Div(decimal.NewFromInt(1_000_000))
	*items = append(*items, BillingItem{
		Type: kind, Quantity: quantity, Unit: billingUnitPerMillionTokens,
		UnitPrice: price.String(), PriceSource: source, Amount: amount.String(),
	})
	return amount
}

func preferredPrice(kind string, preferred, fallback *float64, fallbackSource string) (*float64, string) {
	if preferred != nil {
		return preferred, kind
	}
	return fallback, fallbackSource
}

func boundedTokenCount(value, remaining *uint64) uint64 {
	quantity := tokenCount(value)
	if remaining != nil && quantity > *remaining {
		return *remaining
	}
	return quantity
}

func tokenCount(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func remainingTokens(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
