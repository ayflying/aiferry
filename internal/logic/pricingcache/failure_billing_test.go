package pricingcache

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/usage"
)

func TestEstimateFailureBreakdownKeepsZeroUsageRatesAndRule(t *testing.T) {
	input, output, cached, free := 2.0, 8.0, 0.5, 0.0
	service := New()
	service.snapshot.Store(snapshot{
		"token":   {BillingMode: billingModeToken, Rates: usage.PriceRates{Input: &input, Output: &output, CachedInput: &cached, AudioInput: &free}},
		"rule":    {BillingMode: billingModeRules, Rules: []priceRule{{ID: 8, Name: "套餐", Source: "manual", Priority: 3, Currency: "CNY", Conditions: `{"endpoint":"/chat/completions"}`, Rates: `{"inputPerMillion":2,"outputPerMillion":8,"cachedInputPerMillion":0.5}`}}},
		"request": {BillingMode: billingModeRequest, Rates: usage.PriceRates{Request: &input}},
	})
	for _, model := range []string{"token", "rule", "request"} {
		t.Run(model, func(t *testing.T) {
			b := service.EstimateFailureBreakdown(model, "/chat/completions", usage.TokenUsage{}, estimateTestAt)
			if b == nil || len(b.Items) == 0 {
				t.Fatalf("missing price snapshot for zero usage: %+v", b)
			}
			if model == "rule" && (b.BillingMode != billingModeRules || b.Rule == nil || b.Rule.ID != 8 || b.Rule.Name != "套餐" || b.Currency != "CNY") {
				t.Fatalf("missing rule metadata: %+v", b)
			}
			if model == "token" && len(b.Items) != 4 {
				t.Fatalf("must retain all four configured rates, not fallback-only items: %+v", b.Items)
			}
			for _, item := range b.Items {
				if model != "request" && (item.Quantity != 0 || item.Amount != "0") {
					t.Fatalf("no usage must keep zero quantities and amounts: %+v", item)
				}
			}
			failed := usage.NewFailedBillingDetails(&usage.FailedBillingDetails{BillingMode: b.BillingMode, Currency: b.Currency, Rule: b.Rule, Items: b.Items})
			if failed == nil || !failed.Cost().IsZero() || failed.Charged {
				t.Fatalf("failure must have zero cost: %+v", failed)
			}
		})
	}
	if service.EstimateBreakdown("token", "/chat/completions", usage.TokenUsage{}, estimateTestAt) != nil || service.EstimateBreakdown("rule", "/chat/completions", usage.TokenUsage{}, estimateTestAt) != nil {
		t.Fatal("normal token and rule pricing must still require usage")
	}
	if service.EstimateFailureBreakdown("rule", "/embeddings", usage.TokenUsage{}, estimateTestAt) != nil || service.EstimateFailureBreakdown("unknown", "/chat/completions", usage.TokenUsage{}, estimateTestAt) != nil {
		t.Fatal("failure estimation must still enforce rule matching and model pricing")
	}
}
