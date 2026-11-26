package relay

import (
	"context"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/logic/apikey"
)

// 模型做了映射时，用量日志与扣费都必须按路由模型（对外公开名）计算：
// 客户端可以按上游原始名直接请求，若沿用请求字符串，同一模型会被拆成两个名字统计，
// 并且会让人误以为费用按被路由的上游模型结算。
func TestUsageAccountsByRoutedModelNotUpstream(t *testing.T) {
	if isolatedAtomicUsageTest(t) {
		return
	}
	s, db := newAtomicUsageRelay(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "INSERT INTO model_prices(public_name,billing_mode,request_price) VALUES('gpt-6.1-sol','request',2)"); err != nil {
		t.Fatal(err)
	}
	if err := s.prices.Load(ctx); err != nil {
		t.Fatal(err)
	}
	// 客户端写的是上游原始名，候选声明的是对外公开名。
	err := s.record(ctx, "routed-request", apikey.AuthKey{Id: 1, UserId: 1, KeyHash: "hash"},
		Candidate{PublicName: "gpt-6.1-sol", UpstreamName: "gpt-6.2-sol"}, "", "/chat/completions",
		"gpt-6.2-sol", false, 1, time.Now(), attemptResult{status: 200})
	if err != nil {
		t.Fatal(err)
	}
	var (
		requested string
		upstream  string
		cost      float64
	)
	if err = db.Model("usage_logs").Ctx(ctx).Fields("requested_model", "upstream_model", "estimated_cost").Scan(&struct {
		Requested *string  `orm:"requested_model"`
		Upstream  *string  `orm:"upstream_model"`
		Cost      *float64 `orm:"estimated_cost"`
	}{&requested, &upstream, &cost}); err != nil {
		t.Fatal(err)
	}
	if requested != "gpt-6.1-sol" {
		t.Fatalf("用量日志必须记录路由模型，实际 %q", requested)
	}
	if upstream != "gpt-6.2-sol" {
		t.Fatalf("上游模型只作为诊断字段，实际 %q", upstream)
	}
	if cost != 2 {
		t.Fatalf("扣费必须按路由模型的价格，实际 %v", cost)
	}
	assertAtomicLedger(t, db, 8, 2, 1)
}
