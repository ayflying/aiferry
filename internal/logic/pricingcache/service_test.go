package pricingcache

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/logic/usage"
)

// estimateTestAt 是计费用例的固定评估时刻。这些用例的规则不含时段条件，
// 取任意时刻结果都一样，固定下来只为让用例可复现。
var estimateTestAt = time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)

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

	cost := service.Estimate("cached-model", "/chat/completions", usage.TokenUsage{Input: &input, Output: &output}, estimateTestAt)
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

	cost := service.Estimate("rule-model", "/embeddings", usage.TokenUsage{Input: &input}, estimateTestAt)
	if !service.IsPriced("rule-model") {
		t.Fatal("cached rule should be billable")
	}
	if cost == nil || !cost.Equal(decimal.RequireFromString("0.25")) {
		t.Fatalf("unexpected cached rule cost: %v", cost)
	}
	if unmatched := service.Estimate("rule-model", "/chat/completions", usage.TokenUsage{Input: &input}, estimateTestAt); unmatched != nil {
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

	breakdown := service.EstimateBreakdown("rule-model", "/embeddings", usage.TokenUsage{Input: &input}, estimateTestAt)
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

// TestEstimateBreakdownPicksRuleByTimeWindow 复刻线上故障：
// 高价区规则挂了时段条件、空闲时段规则不限制时段，缓存里按 priority 降序排列。
// 时段条件不被解析时，高价区会在全天命中（18:55 也按高价算），本用例把它钉住。
func TestEstimateBreakdownPicksRuleByTimeWindow(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Shanghai) error = %v", err)
	}
	var input, output uint64 = 1_000_000, 1_000_000
	service := New()
	service.snapshot.Store(snapshot{
		"deepseek-v4.1-flash": {
			BillingMode: billingModeRules,
			Rules: []priceRule{
				{
					ID: 611, Name: "高价区", Source: "manual", Priority: 100, Currency: "CNY",
					Conditions: `{"time":{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","12:00"],["14:00","18:00"]]}}`,
					Rates:      `{"inputPerMillion":0.3,"outputPerMillion":1.2}`,
				},
				{
					ID: 612, Name: "空闲时段", Source: "manual", Priority: 80, Currency: "CNY",
					Conditions: `{"time":{"tz":"Asia/Shanghai"}}`,
					Rates:      `{"inputPerMillion":0.15,"outputPerMillion":0.6}`,
				},
			},
		},
	})

	cases := []struct {
		name     string
		at       time.Time
		wantRule string
		wantCost string
	}{
		{name: "工作日上午高峰", at: time.Date(2026, 9, 14, 10, 0, 0, 0, location), wantRule: "高价区", wantCost: "1.5"},
		{name: "傍晚已过时段", at: time.Date(2026, 9, 14, 18, 55, 0, 0, location), wantRule: "空闲时段", wantCost: "0.75"},
		{name: "午间空档", at: time.Date(2026, 9, 14, 12, 30, 0, 0, location), wantRule: "空闲时段", wantCost: "0.75"},
		{name: "周末全天", at: time.Date(2026, 9, 19, 10, 0, 0, 0, location), wantRule: "空闲时段", wantCost: "0.75"},
	}
	for _, item := range cases {
		breakdown := service.EstimateBreakdown("deepseek-v4.1-flash", "/chat/completions", usage.TokenUsage{Input: &input, Output: &output}, item.at)
		if breakdown == nil || breakdown.Rule == nil {
			t.Fatalf("%s：未命中任何规则", item.name)
		}
		if breakdown.Rule.Name != item.wantRule {
			t.Errorf("%s：命中规则 = %q，期望 %q", item.name, breakdown.Rule.Name, item.wantRule)
		}
		if want := decimal.RequireFromString(item.wantCost); !breakdown.Cost().Equal(want) {
			t.Errorf("%s：成本 = %s，期望 %s", item.name, breakdown.Cost(), want)
		}
	}
}
