package pricingcache

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/service/usage"
)

func TestEstimateUsesCachedTokenPrice(t *testing.T) {
	var (
		input   uint64 = 1_000_000
		output  uint64 = 500_000
		rateIn         = 2.0
		rateOut        = 4.0
	)
	service := New()
	service.snapshot.Store(snapshot{
		"cached-model": {
			BillingMode: billingModeToken,
			Rates:       usage.PriceRates{Input: &rateIn, Output: &rateOut},
		},
	})

	cost := service.Estimate("cached-model", "/chat/completions", usage.TokenUsage{Input: &input, Output: &output})
	if !service.IsPriced("cached-model") {
		t.Fatal("cached token price should be billable")
	}
	if cost == nil || !cost.Equal(decimal.RequireFromString("4")) {
		t.Fatalf("unexpected cached token cost: %v", cost)
	}
}

func TestEstimateUsesCachedRule(t *testing.T) {
	var input uint64 = 1_000_000
	service := New()
	service.snapshot.Store(snapshot{
		"rule-model": {
			BillingMode: billingModeRules,
			Rules: []priceRule{{
				Conditions: `{"endpoint":"/embeddings"}`,
				Rates:      `{"inputPerMillion":0.25}`,
			}},
		},
	})

	cost := service.Estimate("rule-model", "/embeddings", usage.TokenUsage{Input: &input})
	if !service.IsPriced("rule-model") {
		t.Fatal("cached rule should be billable")
	}
	if cost == nil || !cost.Equal(decimal.RequireFromString("0.25")) {
		t.Fatalf("unexpected cached rule cost: %v", cost)
	}
	if unmatched := service.Estimate("rule-model", "/chat/completions", usage.TokenUsage{Input: &input}); unmatched != nil {
		t.Fatalf("unmatched cached rule should not calculate a cost: %v", unmatched)
	}
}

func TestEstimateBreakdownSnapshotsMatchedRule(t *testing.T) {
	var input uint64 = 1_000_000
	service := New()
	service.snapshot.Store(snapshot{
		"rule-model": {
			BillingMode: billingModeRules,
			Rules: []priceRule{{
				ID: 42, Name: "嵌入向量", Source: "manual", Priority: 100, Currency: "CNY",
				Conditions: `{"endpoint":"/embeddings"}`,
				Rates:      `{"inputPerMillion":0.25}`,
			}},
		},
	})

	breakdown := service.EstimateBreakdown("rule-model", "/embeddings", usage.TokenUsage{Input: &input})
	if breakdown == nil || breakdown.BillingMode != billingModeRules || breakdown.Currency != "CNY" {
		t.Fatalf("unexpected rule billing snapshot: %+v", breakdown)
	}
	if breakdown.Rule == nil || breakdown.Rule.ID != 42 || breakdown.Rule.Name != "嵌入向量" || breakdown.Rule.Conditions != `{"endpoint":"/embeddings"}` {
		t.Fatalf("matched rule metadata was not recorded: %+v", breakdown.Rule)
	}
	if !breakdown.Cost().Equal(decimal.RequireFromString("0.25")) {
		t.Fatalf("unexpected rule cost: %s", breakdown.Total)
	}
}
