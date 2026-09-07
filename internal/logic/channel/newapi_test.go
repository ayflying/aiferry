package channel

import "testing"

func TestNewAPIEndpointURL(t *testing.T) {
	value, err := newAPIEndpointURL("https://newapi.example/console/v1", "/api/user/self")
	if err != nil || value != "https://newapi.example/console/api/user/self" {
		t.Fatalf("unexpected NewAPI endpoint: %q %v", value, err)
	}
	if _, err = newAPIEndpointURL("https://newapi.example", "/api/status"); err == nil {
		t.Fatal("expected base URL without /v1 to be rejected")
	}
}

func TestNewAPICostAmounts(t *testing.T) {
	used, remaining, _, err := newAPICostAmounts(
		[]byte(`{"success":true,"data":{"quota":1500000,"used_quota":250000}}`),
		[]byte(`{"success":true,"data":{"quota_per_unit":500000}}`),
	)
	if err != nil || used == nil || remaining == nil || *used != 0.5 || *remaining != 3 {
		t.Fatalf("unexpected NewAPI cost amounts: used=%v remaining=%v err=%v", used, remaining, err)
	}
}

func TestNewAPICostAmountsRejectsMissingQuotaUnit(t *testing.T) {
	_, _, _, err := newAPICostAmounts(
		[]byte(`{"success":true,"data":{"quota":100,"used_quota":50}}`),
		[]byte(`{"success":true,"data":{}}`),
	)
	if err == nil {
		t.Fatal("expected missing quota_per_unit rejection")
	}
}

func TestNewAPISubscriptionAmounts(t *testing.T) {
	used, remaining := newAPISubscriptionAmounts([]byte(`{"success":true,"data":{"subscriptions":[
		{"subscription":{"amount_total":5718000000,"amount_used":569300000}},
		{"subscription":{"amount_total":1000000,"amount_used":250000}}
	]}}`))
	if used != 569550000 || remaining != 5149450000 {
		t.Fatalf("unexpected subscription amounts: used=%v remaining=%v", used, remaining)
	}
}

func TestNewAPISubscriptionAmountsUnlimited(t *testing.T) {
	// amount_total<=0 是不限量订阅：只累计已用，不虚增剩余。
	used, remaining := newAPISubscriptionAmounts([]byte(`{"success":true,"data":{"subscriptions":[
		{"subscription":{"amount_total":0,"amount_used":420000}}
	]}}`))
	if used != 420000 || remaining != 0 {
		t.Fatalf("unexpected unlimited subscription amounts: used=%v remaining=%v", used, remaining)
	}
}

func TestNewAPISubscriptionAmountsFallback(t *testing.T) {
	// 旧版 NewAPI 无订阅接口：success=false 或结构缺失时全部为 0，降级纯钱包。
	used, remaining := newAPISubscriptionAmounts([]byte(`{"success":false,"message":"not found"}`))
	if used != 0 || remaining != 0 {
		t.Fatalf("expected zero fallback amounts: used=%v remaining=%v", used, remaining)
	}
	used, remaining = newAPISubscriptionAmounts([]byte(`{"success":true,"data":{}}`))
	if used != 0 || remaining != 0 {
		t.Fatalf("expected zero amounts for missing subscriptions: used=%v remaining=%v", used, remaining)
	}
}
