package channel

import (
	"encoding/json"
	"testing"
)

func TestModelPriceValuesFromRuleUsesUnconditionalRates(t *testing.T) {
	rule := syncedRule{Conditions: json.RawMessage(`{}`), Rates: json.RawMessage(`{"inputPerMillion":1.25,"cachedInputPerMillion":0.25,"outputPerMillion":5}`)}
	values, ok := modelPriceValuesFromRule(rule)
	if !ok || values.Input == nil || values.CachedInput == nil || values.Output == nil {
		t.Fatalf("expected price values, got %#v", values)
	}
	if *values.Input != 1.25 || *values.CachedInput != 0.25 || *values.Output != 5 {
		t.Fatalf("unexpected price values: %#v", values)
	}
}

func TestModelPriceValuesFromRuleSkipsConditionalRates(t *testing.T) {
	rule := syncedRule{Conditions: json.RawMessage(`{"endpoint":"chat"}`), Rates: json.RawMessage(`{"inputPerMillion":1,"outputPerMillion":2}`)}
	if _, ok := modelPriceValuesFromRule(rule); ok {
		t.Fatal("conditional rules must not overwrite the default public price")
	}
}
