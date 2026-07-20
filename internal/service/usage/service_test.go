package usage

import (
	"strings"
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

func TestNewRequestIDUsesPrefixAndUniqueRandomValue(t *testing.T) {
	first, second := NewRequestID("aftest"), NewRequestID("aftest")
	if !strings.HasPrefix(first, "aftest_") || first == second {
		t.Fatalf("unexpected request IDs: %q, %q", first, second)
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

func TestEstimateBreakdownSeparatesAllBillableComponents(t *testing.T) {
	input, cached, cacheWrite := uint64(1_000_000), uint64(200_000), uint64(100_000)
	imageInput, audioInput := uint64(50_000), uint64(50_000)
	output, audioOutput := uint64(500_000), uint64(100_000)
	inputPrice, cachedPrice, cacheWritePrice := 2.0, 0.5, 3.0
	imagePrice, audioInputPrice, outputPrice, audioOutputPrice, requestPrice := 4.0, 5.0, 8.0, 10.0, 0.01

	breakdown := EstimateBreakdown(TokenUsage{
		Input: &input, CachedInput: &cached, CacheWrite: &cacheWrite, ImageInput: &imageInput,
		AudioInput: &audioInput, Output: &output, AudioOutput: &audioOutput,
	}, PriceRates{
		Input: &inputPrice, CachedInput: &cachedPrice, CacheWrite: &cacheWritePrice,
		ImageInput: &imagePrice, AudioInput: &audioInputPrice, Output: &outputPrice,
		AudioOutput: &audioOutputPrice, Request: &requestPrice,
	})
	if breakdown == nil {
		t.Fatal("expected billing breakdown")
	}
	if !breakdown.Cost().Equal(decimal.RequireFromString("6.26")) {
		t.Fatalf("unexpected total: %s", breakdown.Total)
	}
	if len(breakdown.Items) != 8 {
		t.Fatalf("expected eight billed components, got %+v", breakdown.Items)
	}
	if breakdown.Items[0].Type != "cached_input" || breakdown.Items[0].Quantity != cached || breakdown.Items[0].Amount != "0.1" {
		t.Fatalf("unexpected cached input item: %+v", breakdown.Items[0])
	}
	if breakdown.Items[4].Type != "input" || breakdown.Items[4].Quantity != 600_000 || breakdown.Items[4].Amount != "1.2" {
		t.Fatalf("unexpected remaining input item: %+v", breakdown.Items[4])
	}
	if breakdown.Items[6].Type != "output" || breakdown.Items[6].Quantity != 400_000 || breakdown.Items[6].Amount != "3.2" {
		t.Fatalf("unexpected remaining output item: %+v", breakdown.Items[6])
	}
	if breakdown.Items[7].Type != "request" || breakdown.Items[7].Amount != "0.01" {
		t.Fatalf("unexpected request item: %+v", breakdown.Items[7])
	}
}

func TestEstimateBreakdownKeepsFallbackAndSettlementRounding(t *testing.T) {
	input, cached := uint64(100), uint64(20)
	inputPrice := 2.33333333
	breakdown := EstimateBreakdown(TokenUsage{Input: &input, CachedInput: &cached}, PriceRates{Input: &inputPrice})
	if breakdown == nil {
		t.Fatal("expected billing breakdown")
	}
	if breakdown.Items[0].PriceSource != "input" || breakdown.Items[0].Amount != "0.0000466666666" {
		t.Fatalf("cached input should fall back to input price: %+v", breakdown.Items[0])
	}
	if breakdown.Total != "0.00023333" {
		t.Fatalf("unexpected settled total: %s", breakdown.Total)
	}
	if len(breakdown.Items) != 3 || breakdown.Items[2].Type != "rounding" {
		t.Fatalf("expected settlement adjustment: %+v", breakdown.Items)
	}
	encoded, err := breakdown.JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored := ParseBillingBreakdown(encoded)
	if restored == nil || restored.Total != breakdown.Total || len(restored.Items) != len(breakdown.Items) {
		t.Fatalf("billing snapshot was not preserved: %+v", restored)
	}
}
