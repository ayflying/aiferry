package relay

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/yunloli/aiferry/internal/service/usage"
)

func TestParseJSONUsageVariants(t *testing.T) {
	tokens := parseJSONUsage([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30},"total_tokens":120}}`))
	if tokens.Input == nil || *tokens.Input != 100 || tokens.Output == nil || *tokens.Output != 20 || tokens.CachedInput == nil || *tokens.CachedInput != 30 {
		t.Fatalf("unexpected chat usage: %+v", tokens)
	}
	tokens = parseJSONUsage([]byte(`{"usage":{"input_tokens":12,"output_tokens":8}}`))
	if tokens.Total == nil || *tokens.Total != 20 {
		t.Fatalf("total should be derived: %+v", tokens)
	}
}

func TestParseSSEUsage(t *testing.T) {
	var tokens = parseJSONUsage(nil)
	parseSSEUsage([]byte("data: {\"usage\":{\"input_tokens\":9,\"output_tokens\":3,\"total_tokens\":12}}\n"), &tokens)
	if tokens.Total == nil || *tokens.Total != 12 {
		t.Fatalf("unexpected SSE usage: %+v", tokens)
	}
}

func TestWeightedOrderKeepsPriorityGroups(t *testing.T) {
	input := []Candidate{{ChannelID: 1, Priority: 5, Weight: 1}, {ChannelID: 2, Priority: 10, Weight: 2}, {ChannelID: 3, Priority: 5, Weight: 3}, {ChannelID: 4, Priority: 10, Weight: 1}}
	ordered := weightedOrder(input)
	if len(ordered) != len(input) {
		t.Fatalf("candidate count changed: %d", len(ordered))
	}
	if ordered[0].Priority != 10 || ordered[1].Priority != 10 || ordered[2].Priority != 5 || ordered[3].Priority != 5 {
		t.Fatalf("priority order changed: %+v", ordered)
	}
}

func TestRetryableStatus(t *testing.T) {
	for _, status := range []int{401, 403, 404, 408, 429, 500, 503} {
		if !retryableStatus(status) {
			t.Fatalf("status %d should retry", status)
		}
	}
	for _, status := range []int{200, 400, 422} {
		if retryableStatus(status) {
			t.Fatalf("status %d should not retry", status)
		}
	}
}

func TestRuleCostHonorsEndpointAndCachedInput(t *testing.T) {
	input, cached, output := uint64(1_000_000), uint64(200_000), uint64(500_000)
	cost, ok := ruleCost(`{"endpoint":"/chat/completions","inputTokensAtLeast":500000}`, `{"inputPerMillion":2,"cachedInputPerMillion":0.5,"outputPerMillion":8,"request":0.01}`, "/chat/completions", usage.TokenUsage{Input: &input, CachedInput: &cached, Output: &output})
	if !ok || !cost.Equal(decimalRequire("5.71")) {
		t.Fatalf("unexpected rule cost: %v, matched=%t", cost, ok)
	}
	if _, ok = ruleCost(`{"endpoint":"/embeddings"}`, `{"inputPerMillion":2,"outputPerMillion":8}`, "/chat/completions", usage.TokenUsage{Input: &input, Output: &output}); ok {
		t.Fatal("endpoint-mismatched rule should not apply")
	}
}

func decimalRequire(value string) decimal.Decimal {
	result, err := decimal.NewFromString(value)
	if err != nil {
		panic(err)
	}
	return result
}
