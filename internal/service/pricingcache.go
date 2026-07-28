// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	"github.com/shopspring/decimal"
	_ "github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

type (
	IPricingCache interface {
		Load(ctx context.Context) error
		IsPriced(modelName string) bool
		EstimateBreakdown(modelName string, endpoint string, tokens usage.TokenUsage) *usage.BillingBreakdown
		Estimate(modelName string, endpoint string, tokens usage.TokenUsage) *decimal.Decimal
	}
)

var (
	localPricingCache IPricingCache
)

func PricingCache() IPricingCache {
	if localPricingCache == nil {
		panic("implement not found for interface IPricingCache, forgot register?")
	}
	return localPricingCache
}

func RegisterPricingCache(i IPricingCache) {
	localPricingCache = i
}
