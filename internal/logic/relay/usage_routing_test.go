package relay

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

// 模型做了映射时，用量日志与扣费都必须按路由模型（对外公开名）计算：
// 客户端可以按上游原始名直接请求，若沿用请求字符串，同一模型会被拆成两个名字统计，
// 并且会让人误以为费用按被路由的上游模型结算。
func TestUsageAccountsByRoutedModelNotUpstream(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if _, err := db.Exec(context.Background(), "INSERT INTO model_prices(public_name,billing_mode,request_price) VALUES('gpt-6.1-sol','request',2)"); err != nil {
		t.Fatal(err)
	}
	if err := s.prices.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 客户端写的是上游原始名，候选声明的是对外公开名。
	recordRoutedUsage(t, s, Candidate{PublicName: "gpt-6.1-sol", UpstreamName: "gpt-6.2-sol"}, "gpt-6.2-sol")
	if requested := usageLogValue(t, db, "requested_model").String(); requested != "gpt-6.1-sol" {
		t.Fatalf("用量日志必须记录路由模型，实际 %q", requested)
	}
	if upstream := usageLogValue(t, db, "upstream_model").String(); upstream != "gpt-6.2-sol" {
		t.Fatalf("上游模型只作为诊断字段，实际 %q", upstream)
	}
	if cost := usageLogValue(t, db, "estimated_cost").Float64(); cost != 2 {
		t.Fatalf("扣费必须按路由模型的价格，实际 %v", cost)
	}
	if priced := usageLogPricedModel(t, db); priced != "gpt-6.1-sol" {
		t.Fatalf("账单快照应记录按公开名取价，实际 %q", priced)
	}
	assertAtomicLedger(t, db, 8, 2, 1)
}

// 对外公开名还没维护价格时，按上游模型价格兜底，避免整类请求被静默算成免费；
// 日志里的模型名仍然是路由模型，账单快照额外记录实际取价的模型。
func TestUsageFallsBackToUpstreamPriceWhenPublicUnpriced(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	if _, err := db.Exec(context.Background(), "INSERT INTO model_prices(public_name,billing_mode,request_price) VALUES('gpt-6.2-sol','request',3)"); err != nil {
		t.Fatal(err)
	}
	if err := s.prices.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	recordRoutedUsage(t, s, Candidate{PublicName: "gpt-6.1-sol", UpstreamName: "gpt-6.2-sol"}, "gpt-6.1-sol")
	if requested := usageLogValue(t, db, "requested_model").String(); requested != "gpt-6.1-sol" {
		t.Fatalf("日志仍应记录路由模型，实际 %q", requested)
	}
	if cost := usageLogValue(t, db, "estimated_cost").Float64(); cost != 3 {
		t.Fatalf("公开名无价格时应回退上游名价格，实际 %v", cost)
	}
	if priced := usageLogPricedModel(t, db); priced != "gpt-6.2-sol" {
		t.Fatalf("账单快照应记录回退后的计价模型，实际 %q", priced)
	}
	assertAtomicLedger(t, db, 7, 3, 1)
}

func recordRoutedUsage(t *testing.T, s *sRelay, candidate Candidate, requestedModel string) {
	t.Helper()
	err := s.record(context.Background(), "routed-request", apikey.AuthKey{Id: 1, UserId: 1, KeyHash: "hash"},
		candidate, "", "/chat/completions", requestedModel, false, 1, time.Now(), attemptResult{status: 200})
	if err != nil {
		t.Fatal(err)
	}
}

func usageLogValue(t *testing.T, db gdb.DB, column string) gdb.Value {
	t.Helper()
	value, err := db.GetValue(context.Background(), "SELECT "+column+" FROM usage_logs")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func usageLogPricedModel(t *testing.T, db gdb.DB) string {
	t.Helper()
	raw := usageLogValue(t, db, "billing_details_json")
	if !raw.IsNil() {
		var details usage.BillingBreakdown
		if err := json.Unmarshal(raw.Bytes(), &details); err == nil {
			return details.PricedModel
		}
	}
	return ""
}
