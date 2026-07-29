package channel

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCostRangeDefaultsToCurrentDay(t *testing.T) {
	start, end, err := costRange("", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if start.Year() != now.Year() || start.Month() != now.Month() || start.Day() != now.Day() ||
		start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || !end.After(start) {
		t.Fatalf("unexpected range: %v - %v", start, end)
	}
}

func TestJSONFloatPaths(t *testing.T) {
	body := []byte(`{"usage":{"cost":"12.34"},"remaining":8.5}`)
	used := jsonFloat(body, "usage.cost")
	remaining := firstFloat(body, "missing", "remaining")
	if used == nil || *used != 12.34 || remaining == nil || *remaining != 8.5 {
		t.Fatalf("unexpected values: %v %v", used, remaining)
	}
}

func TestResolveEndpointURL(t *testing.T) {
	value, err := resolveEndpointURL("https://relay.example/v1", "usage")
	if err != nil || value != "https://relay.example/v1/usage" {
		t.Fatalf("unexpected URL: %q %v", value, err)
	}
	value, err = resolveEndpointURL("https://relay.example/v1", "/models")
	if err != nil || value != "https://relay.example/v1/models" {
		t.Fatalf("unexpected leading-slash URL: %q %v", value, err)
	}
}

func TestCostResultDoesNotFlattenMixedCurrencies(t *testing.T) {
	usd, cny := 2.5, 18.0
	result := CostResult{Summaries: []CostSummary{
		{Currency: "USD", RemainingAmount: &usd},
		{Currency: "CNY", RemainingAmount: &cny},
	}}
	result.applySingleSummary()
	if result.Currency != "" || result.RemainingAmount != nil || result.UsedAmount != nil {
		t.Fatalf("mixed currencies must remain grouped: %+v", result)
	}
}

func TestCostResultUsesSingleCurrencySummary(t *testing.T) {
	used, remaining := 1.25, 8.75
	result := CostResult{Summaries: []CostSummary{{Currency: "USD", UsedAmount: &used, RemainingAmount: &remaining}}}
	result.applySingleSummary()
	if result.Currency != "USD" || result.UsedAmount == nil || *result.UsedAmount != used || result.RemainingAmount == nil || *result.RemainingAmount != remaining {
		t.Fatalf("single currency summary was not exposed: %+v", result)
	}
}

func TestApplyTrackedCostPreservesUnknownRemainingBalance(t *testing.T) {
	used := 3.5
	updatedUsed, remaining, currency := applyTrackedCost(&used, nil, "", decimal.RequireFromString("1.25"))
	if updatedUsed != 4.75 || remaining != nil || currency != "USD" {
		t.Fatalf("unexpected tracked cost update: used=%v remaining=%v currency=%q", updatedUsed, remaining, currency)
	}
	currentRemaining := 1.0
	_, remaining, currency = applyTrackedCost(nil, &currentRemaining, "usd", decimal.RequireFromString("1.25"))
	if remaining == nil || *remaining != 0 || currency != "usd" {
		t.Fatalf("unexpected remaining balance update: remaining=%v currency=%q", remaining, currency)
	}
}
