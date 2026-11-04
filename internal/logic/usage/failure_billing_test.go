package usage

import (
	"context"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

func failedBreakdownInput() *FailedBillingDetails {
	return &FailedBillingDetails{
		BillingMode: "rules",
		Currency:    "CNY",
		Rule:        &BillingRuleSnapshot{ID: 7, Name: "夜间套餐", Source: "manual", Priority: 2, Conditions: `{"endpoint":"/chat/completions"}`},
		Items: []BillingItem{
			{Type: "input", Quantity: 1_000_000, Unit: billingUnitPerMillionTokens, UnitPrice: "2", Amount: "2"},
			{Type: "output", Quantity: 500_000, Unit: billingUnitPerMillionTokens, UnitPrice: "8", Amount: "4"},
		},
		Subtotal: "6", Total: "6",
	}
}

func TestNewFailedBillingDetailsZerosCostAndKeepsRule(t *testing.T) {
	input := failedBreakdownInput()
	breakdown := NewFailedBillingDetails(input)
	if breakdown == nil || breakdown.BillingMode != "rules" || breakdown.Currency != "CNY" || breakdown.Charged {
		t.Fatalf("unexpected failure snapshot: %+v", breakdown)
	}
	if !reflect.DeepEqual(breakdown.Rule, input.Rule) || breakdown.Rule == input.Rule {
		t.Fatalf("rule must be preserved as an independent snapshot: %+v", breakdown.Rule)
	}
	if breakdown.Total != "0.00000000" || breakdown.Subtotal != "0" || !breakdown.Cost().IsZero() {
		t.Fatalf("failed amounts must be zero: %+v", breakdown)
	}
	for i, item := range breakdown.Items {
		if item.Amount != "0" || item.Quantity != input.Items[i].Quantity || item.UnitPrice != input.Items[i].UnitPrice {
			t.Fatalf("only item amount should change: %+v", item)
		}
	}
	breakdown.Rule.Name = "changed"
	if input.Rule.Name != "夜间套餐" || input.Items[0].Amount != "2" || input.Total != "6" {
		t.Fatalf("input snapshot was mutated: %+v", input)
	}
}

func TestNewFailedBillingDetailsKeepsZeroQuantityItems(t *testing.T) {
	breakdown := NewFailedBillingDetails(&FailedBillingDetails{
		Items: []BillingItem{
			{Type: "cached_input", Quantity: 0, Unit: billingUnitPerMillionTokens, UnitPrice: "0.5", Amount: "0"},
			{Type: "input", Quantity: 1000, Unit: billingUnitPerMillionTokens, UnitPrice: "2", Amount: "0.002"},
			{Type: "request", Quantity: 1, Unit: billingUnitPerRequest, UnitPrice: "0.01", Amount: "0.01"},
		},
	})
	if breakdown == nil || len(breakdown.Items) != 3 || breakdown.Items[0].Quantity != 0 || breakdown.Items[0].UnitPrice != "0.5" {
		t.Fatalf("zero-quantity rates must be retained: %+v", breakdown)
	}
	if breakdown.BillingMode != "token" || breakdown.Currency != "USD" || !breakdown.Cost().IsZero() {
		t.Fatalf("unexpected defaults or cost: %+v", breakdown)
	}
}

func TestNewFailedBillingDetailsRequiresPriceMetadata(t *testing.T) {
	if NewFailedBillingDetails(nil) != nil || NewFailedBillingDetails(&FailedBillingDetails{}) != nil {
		t.Fatal("missing price metadata must not create a snapshot")
	}
	if NewFailedBillingDetails(&FailedBillingDetails{Items: []BillingItem{{Type: "input", Quantity: 0, UnitPrice: "2", Amount: "0"}}}) == nil {
		t.Fatal("zero quantity with a rate must create a snapshot")
	}
	if NewFailedBillingDetails(&FailedBillingDetails{BillingMode: "rules", Rule: &BillingRuleSnapshot{ID: 1}}) == nil {
		t.Fatal("rule metadata alone must be retained")
	}
}

func TestFailureCostIsExactlyZeroIncludingDatabaseRoundTrip(t *testing.T) {
	if !FailureCost().IsZero() || FailureCost().StringFixed(8) != "0.00000000" {
		t.Fatalf("failure cost must be real zero, not a marker: %s", FailureCost())
	}
	stored, err := decimal.NewFromString(FailureCost().StringFixed(8))
	if err != nil || !stored.IsZero() {
		t.Fatalf("database decimal round trip must remain zero: %s, %v", stored, err)
	}
}

func TestReconstructFailureBillingDetailsWithNilAndLegacyCosts(t *testing.T) {
	zero, oldMarker, oldCost := 0.0, 0.00000001, 6.0
	existing := &BillingBreakdown{Total: "0.00000000"}
	items := []LogView{
		{HttpStatus: 502},
		{HttpStatus: 500, EstimatedCost: &zero},
		{HttpStatus: 429, EstimatedCost: &oldMarker},
		{HttpStatus: 503, EstimatedCost: &oldCost},
		{HttpStatus: 200},
		{HttpStatus: 299},
		{HttpStatus: 502, BillingDetails: existing},
	}
	calls := 0
	reconstructFailureDetails(context.Background(), items, func(context.Context, LogView) *BillingBreakdown {
		calls++
		input := failedBreakdownInput()
		return &BillingBreakdown{BillingMode: input.BillingMode, Currency: input.Currency, Rule: input.Rule, Items: input.Items, Subtotal: input.Subtotal, Total: input.Total}
	})
	if calls != 4 {
		t.Fatalf("estimate calls = %d, want 4 failures without snapshots", calls)
	}
	for _, item := range items[:4] {
		b := item.BillingDetails
		if b == nil || !b.Reconstructed || b.Charged || b.BillingMode != "rules" || b.Rule.ID != 7 || !b.Cost().IsZero() || b.Items[0].Amount != "0" {
			t.Fatalf("unexpected reconstructed failure: %+v", b)
		}
	}
	if items[0].EstimatedCost != nil || oldMarker != 0.00000001 || oldCost != 6 || items[4].BillingDetails != nil || items[5].BillingDetails != nil || items[6].BillingDetails != existing {
		t.Fatal("reconstruction must not alter stored costs, success logs or existing snapshots")
	}
}

func TestReconstructFailureBillingDetailsWithoutPrice(t *testing.T) {
	items := []LogView{{HttpStatus: 502}}
	reconstructFailureDetails(context.Background(), items, func(context.Context, LogView) *BillingBreakdown { return nil })
	if items[0].BillingDetails != nil {
		t.Fatal("unpriced failure must not get an invented snapshot")
	}
}

func TestFailedBillingRoundTripThroughSnapshot(t *testing.T) {
	encoded, err := NewFailedBillingDetails(failedBreakdownInput()).JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored := ParseBillingBreakdown(encoded)
	if restored == nil || restored.BillingMode != "rules" || restored.Charged || restored.Rule == nil || restored.Rule.ID != 7 {
		t.Fatalf("failed price metadata was not preserved: %+v", restored)
	}
	if restored.Total != "0.00000000" || !restored.Cost().IsZero() || restored.Items[0].Amount != "0" {
		t.Fatalf("restored failure must be free: %+v", restored)
	}
}
