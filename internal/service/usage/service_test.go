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
	cost := EstimateCost(TokenUsage{Input: &input, CachedInput: &cached, Output: &output}, PriceRates{Input: &inputPrice, CachedInput: &cachedPrice, Output: &outputPrice})
	if cost == nil || !cost.Equal(decimal.NewFromFloat(5.625)) {
		t.Fatalf("unexpected cost: %v", cost)
	}
}

func TestEstimateCostRequiresPrices(t *testing.T) {
	input := uint64(10)
	output := uint64(5)
	price := 1.0
	if cost := EstimateCost(TokenUsage{Input: &input, Output: &output}, PriceRates{Input: &price}); cost == nil || !cost.Equal(decimal.NewFromFloat(0.00001)) {
		t.Fatalf("configured input pricing should still be applied: %v", cost)
	}
}

func TestEstimateCostUsesRequestAndSpecialTokenPrices(t *testing.T) {
	input, cacheWrite, audioOutput := uint64(100), uint64(20), uint64(30)
	inputPrice, cacheWritePrice, audioOutputPrice, requestPrice := 2.0, 6.0, 8.0, 0.01
	cost := EstimateCost(TokenUsage{Input: &input, CacheWrite: &cacheWrite, AudioOutput: &audioOutput}, PriceRates{
		Input: &inputPrice, CacheWrite: &cacheWritePrice, AudioOutput: &audioOutputPrice, Request: &requestPrice,
	})
	if cost == nil || !cost.Equal(decimal.RequireFromString("0.01052")) {
		t.Fatalf("unexpected special price cost: %v", cost)
	}
}
