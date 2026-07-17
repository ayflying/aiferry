package channel

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestModelPriceValuesFromRuleUsesUnconditionalRates(t *testing.T) {
	rule := syncedRule{Conditions: json.RawMessage(`{}`), Rates: json.RawMessage(`{"inputPerMillion":1.25,"cachedInputPerMillion":0.25,"cacheWritePerMillion":3,"outputPerMillion":5,"imageInputPerMillion":4,"audioInputPerMillion":2,"audioOutputPerMillion":6,"request":0.01}`)}
	values, ok := modelPriceValuesFromRule(rule)
	if !ok || values.Input == nil || values.CachedInput == nil || values.CacheWrite == nil || values.Output == nil || values.ImageInput == nil || values.AudioInput == nil || values.AudioOutput == nil || values.Request == nil {
		t.Fatalf("expected price values, got %#v", values)
	}
	if *values.Input != 1.25 || *values.CachedInput != 0.25 || *values.CacheWrite != 3 || *values.Output != 5 || *values.ImageInput != 4 || *values.AudioInput != 2 || *values.AudioOutput != 6 || *values.Request != 0.01 {
		t.Fatalf("unexpected price values: %#v", values)
	}
}

func TestModelPriceValuesFromRuleSkipsConditionalRates(t *testing.T) {
	rule := syncedRule{Conditions: json.RawMessage(`{"endpoint":"chat"}`), Rates: json.RawMessage(`{"inputPerMillion":1,"outputPerMillion":2}`)}
	if _, ok := modelPriceValuesFromRule(rule); ok {
		t.Fatal("conditional rules must not overwrite the default public price")
	}
}

func TestSyncedRulesFromNewAPIRatioConvertsToUSDPerMillion(t *testing.T) {
	rules, err := syncedRulesFromNewAPIRatio([]byte(`{
  "data": {
    "model_ratio": {"gpt-5": 0.625, "free-model": 0, "image-model": 1},
    "model_price": {"image-model": 0.08},
    "cache_ratio": {"gpt-5": 0.1},
    "completion_ratio": {"gpt-5": 8}
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 || rules[0].Model != "free-model" || rules[1].Model != "gpt-5" || rules[2].Model != "image-model" {
		t.Fatalf("unexpected rules: %#v", rules)
	}
	var rates map[string]float64
	if err = json.Unmarshal(rules[1].Rates, &rates); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"inputPerMillion": 1.25, "cachedInputPerMillion": 0.125, "outputPerMillion": 10}
	if !reflect.DeepEqual(rates, want) {
		t.Fatalf("unexpected converted rates: %#v", rates)
	}
	rates = nil
	if err = json.Unmarshal(rules[2].Rates, &rates); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rates, map[string]float64{"request": 0.08}) {
		t.Fatalf("unexpected request price: %#v", rates)
	}
}

func TestSyncedRulesFromNewAPIRatioRequiresModelRatios(t *testing.T) {
	if _, err := syncedRulesFromNewAPIRatio([]byte(`{"data":{"completion_ratio":{"gpt-5":8}}}`)); err == nil {
		t.Fatal("expected missing model_ratio to fail")
	}
}
