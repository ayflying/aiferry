package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/usage"
)

func TestFailureTestBillingKeepsRuleAndZeroQuantity(t *testing.T) {
	original := &usage.BillingBreakdown{BillingMode: "rules", Currency: "CNY", Rule: &usage.BillingRuleSnapshot{ID: 9, Name: "套餐"}, Items: []usage.BillingItem{{Type: "input", Quantity: 0, UnitPrice: "2", Amount: "0"}}, Total: "2.00000000"}
	cost, details := failureTestBilling(original)
	if cost == nil || !cost.IsZero() || details == nil || !details.Cost().IsZero() || details.Charged || details.BillingMode != "rules" || details.Rule == nil || details.Rule.ID != 9 || len(details.Items) != 1 || details.Items[0].Quantity != 0 || details.Items[0].UnitPrice != "2" {
		t.Fatalf("failed snapshot lost rule/rate or retained cost: cost=%v, details=%+v", cost, details)
	}
	if original.Total != "2.00000000" || original.Rule == details.Rule {
		t.Fatal("original snapshot must be independent")
	}
}
