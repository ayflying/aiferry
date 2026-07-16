package usage

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestEstimateCostWithCachedTokens(t *testing.T) {
	input := uint64(1_000_000)
	cached := uint64(250_000)
	output := uint64(500_000)
	inputPrice := 2.0
	cachedPrice := 0.5
	outputPrice := 8.0
	cost := EstimateCost(TokenUsage{Input: &input, CachedInput: &cached, Output: &output}, &inputPrice, &cachedPrice, &outputPrice)
	if cost == nil || !cost.Equal(decimal.NewFromFloat(5.625)) {
		t.Fatalf("unexpected cost: %v", cost)
	}
}

func TestEstimateCostRequiresPrices(t *testing.T) {
	input := uint64(10)
	output := uint64(5)
	price := 1.0
	if cost := EstimateCost(TokenUsage{Input: &input, Output: &output}, &price, nil, nil); cost != nil {
		t.Fatalf("missing output price should remain unpriced: %v", cost)
	}
}
