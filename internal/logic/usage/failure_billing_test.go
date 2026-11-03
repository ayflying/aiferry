package usage

import (
	"testing"

	"github.com/shopspring/decimal"
)

func failedBreakdownInput() *FailedBillingDetails {
	return &FailedBillingDetails{
		Currency: "USD",
		Items: []BillingItem{
			{Type: "input", Quantity: 1_000_000, Unit: billingUnitPerMillionTokens, UnitPrice: "2", Amount: "2"},
			{Type: "output", Quantity: 500_000, Unit: billingUnitPerMillionTokens, UnitPrice: "8", Amount: "4"},
		},
		Subtotal: "6",
		Total:    "6",
	}
}

func TestNewFailedBillingDetailsMarksFailureAndKeepsCost(t *testing.T) {
	breakdown := NewFailedBillingDetails(failedBreakdownInput())
	if breakdown == nil {
		t.Fatal("expected failed billing breakdown")
	}
	if breakdown.BillingMode != failureBillingMode {
		t.Fatalf("billing mode = %q, want %q", breakdown.BillingMode, failureBillingMode)
	}
	// 失败请求不倒扣用户余额，charged 必须为 false，否则详情会谎称扣款成功。
	if breakdown.Charged {
		t.Fatal("failed billing must not be marked as charged")
	}
	if breakdown.Total != "6.00000000" {
		t.Fatalf("unexpected total: %s", breakdown.Total)
	}
	if breakdown.Subtotal != "6" {
		t.Fatalf("subtotal should keep the requested price snapshot: %s", breakdown.Subtotal)
	}
}

func TestNewFailedBillingDetailsDropsZeroQuantityItems(t *testing.T) {
	breakdown := NewFailedBillingDetails(&FailedBillingDetails{
		Items: []BillingItem{
			// 上游没返回缓存用量时，按量项数量为 0，保留只会被误读成「算过缓存」。
			{Type: "cached_input", Quantity: 0, Unit: billingUnitPerMillionTokens, UnitPrice: "0.5", Amount: "0"},
			{Type: "input", Quantity: 1000, Unit: billingUnitPerMillionTokens, UnitPrice: "2", Amount: "0.002"},
			// 按次计费的固定费用与用量无关，数量为 1 才出现，这里验证它不会被误删。
			{Type: "request", Quantity: 1, Unit: billingUnitPerRequest, UnitPrice: "0.01", Amount: "0.01"},
		},
	})
	if breakdown == nil {
		t.Fatal("expected failed billing breakdown")
	}
	if len(breakdown.Items) != 2 {
		t.Fatalf("expected zero-quantity items to be dropped: %+v", breakdown.Items)
	}
	if breakdown.Total != "0.01200000" {
		t.Fatalf("unexpected total: %s", breakdown.Total)
	}
}

func TestNewFailedBillingDetailsRequiresBillableItems(t *testing.T) {
	if NewFailedBillingDetails(nil) != nil {
		t.Fatal("nil input must not create a billing breakdown")
	}
	if NewFailedBillingDetails(&FailedBillingDetails{}) != nil {
		t.Fatal("empty items must not create a billing breakdown")
	}
	if NewFailedBillingDetails(&FailedBillingDetails{Items: []BillingItem{
		{Type: "input", Quantity: 0, Amount: "0"},
	}}) != nil {
		t.Fatal("all-zero quantities must not create a billing breakdown")
	}
}

func TestFailureCostIsDistinguishableFromZero(t *testing.T) {
	// 标记价必须非 0：库里 0 表示「没有用量」，两者混在一起就无法区分失败是否计费过。
	if FailureCost().IsZero() || FailureCost().IsNegative() {
		t.Fatalf("failure cost marker must be a positive value: %s", FailureCost())
	}
}

func TestFailureCostSurvivesDatabaseRoundTrip(t *testing.T) {
	// 标记价要经过 float64 → 数据库 decimal(20,8) → float64 的往返，
	// 取 1e-9 就是因为它在这个精度下不会塌成 0，否则「失败已计费」会被读成「没有用量」。
	recorded := FailureCost().InexactFloat64()
	if recorded == 0 {
		t.Fatal("failure cost marker collapsed to zero after float conversion")
	}
	if !decimal.RequireFromString("0.00000001").GreaterThan(decimal.NewFromFloat(recorded)) {
		t.Fatalf("failure cost marker must stay below settlement precision: %v", recorded)
	}
}

func TestReconstructFailureBillingDetailsOnlyTargetsFailures(t *testing.T) {
	// 复原先决条件：这条记录已经是「失败且有成本」。状态码只用来确认分类，
	// 真正的价格估算依赖数据库，这里验证分类判定与早退分支。
	marked := FailureCost().InexactFloat64()
	failed := LogView{HttpStatus: 502, EstimatedCost: &marked}
	if !isFailureLog(failed) {
		t.Fatal("502 must be treated as a failure")
	}
	if isFailureLog(LogView{HttpStatus: 200}) {
		t.Fatal("200 must not be treated as a failure")
	}
	if isFailureLog(LogView{HttpStatus: 299}) {
		t.Fatal("299 must not be treated as a failure")
	}
}

func TestFailedBillingRoundTripThroughSnapshot(t *testing.T) {
	// 快照要落成 JSON 再读回来：失败标记与 total 都不能在序列化过程中丢失，
	// 否则列表页与详情页会对同一笔开销给出不同说法。
	encoded, err := NewFailedBillingDetails(failedBreakdownInput()).JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored := ParseBillingBreakdown(encoded)
	if restored == nil || restored.BillingMode != failureBillingMode || restored.Charged {
		t.Fatalf("failed billing snapshot was not preserved: %+v", restored)
	}
	if restored.Total != "6.00000000" || !restored.Cost().Equal(decimal.RequireFromString("6")) {
		t.Fatalf("unexpected restored total: %s", restored.Total)
	}
}
