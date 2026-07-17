package usage

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/dao"
)

type publicModelPrice struct {
	PublicName       string   `orm:"public_name"`
	BillingMode      string   `orm:"billing_mode"`
	InputPrice       *float64 `orm:"input_price"`
	CachedInputPrice *float64 `orm:"cached_input_price"`
	CacheWritePrice  *float64 `orm:"cache_write_price"`
	OutputPrice      *float64 `orm:"output_price"`
	ImageInputPrice  *float64 `orm:"image_input_price"`
	AudioInputPrice  *float64 `orm:"audio_input_price"`
	AudioOutputPrice *float64 `orm:"audio_output_price"`
	RequestPrice     *float64 `orm:"request_price"`
}

func EstimatePublicModelCost(ctx context.Context, modelName, endpoint string, tokens TokenUsage) *decimal.Decimal {
	var price publicModelPrice
	if err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Scan(&price); err != nil || price.PublicName == "" {
		return nil
	}
	switch price.BillingMode {
	case "rules":
		return EstimateRuleCost(ctx, modelName, endpoint, tokens)
	case "request":
		return EstimateCost(tokens, PriceRates{Request: price.RequestPrice})
	default:
		return EstimateCost(tokens, PriceRates{
			Input:       price.InputPrice,
			CachedInput: price.CachedInputPrice,
			CacheWrite:  price.CacheWritePrice,
			Output:      price.OutputPrice,
			ImageInput:  price.ImageInputPrice,
			AudioInput:  price.AudioInputPrice,
			AudioOutput: price.AudioOutputPrice,
		})
	}
}

func EstimateRuleCost(ctx context.Context, modelName, endpoint string, tokens TokenUsage) *decimal.Decimal {
	var rules []struct {
		ConditionsJSON string `orm:"conditions_json"`
		RatesJSON      string `orm:"rates_json"`
	}
	err := dao.ModelPriceRules.Ctx(ctx).
		Fields("conditions_json,rates_json").
		Where("model_name", modelName).
		Where("status", 1).
		OrderDesc("priority").
		OrderDesc("source = 'manual'").
		OrderDesc("id").
		Scan(&rules)
	if err != nil {
		return nil
	}
	for _, rule := range rules {
		if cost, ok := RuleCost(rule.ConditionsJSON, rule.RatesJSON, endpoint, tokens); ok {
			return cost
		}
	}
	return nil
}

func RuleCost(conditionsJSON, ratesJSON, endpoint string, tokens TokenUsage) (*decimal.Decimal, bool) {
	conditions := gjson.Parse(conditionsJSON)
	if configured := strings.TrimSpace(conditions.Get("endpoint").String()); configured != "" && configured != endpoint {
		return nil, false
	}
	input, output := tokenValue(tokens.Input), tokenValue(tokens.Output)
	if !matchesTokenRange(conditions, "inputTokens", input) || !matchesTokenRange(conditions, "outputTokens", output) || !matchesTokenRange(conditions, "totalTokens", input+output) {
		return nil, false
	}
	rates := gjson.Parse(ratesJSON)
	cost := EstimateCost(tokens, PriceRates{
		Input:       priceRate(rates.Get("inputPerMillion")),
		CachedInput: priceRate(rates.Get("cachedInputPerMillion")),
		CacheWrite:  priceRate(rates.Get("cacheWritePerMillion")),
		Output:      priceRate(rates.Get("outputPerMillion")),
		ImageInput:  priceRate(rates.Get("imageInputPerMillion")),
		AudioInput:  priceRate(rates.Get("audioInputPerMillion")),
		AudioOutput: priceRate(rates.Get("audioOutputPerMillion")),
		Request:     priceRate(rates.Get("request")),
	})
	return cost, cost != nil
}

func matchesTokenRange(conditions gjson.Result, prefix string, value uint64) bool {
	if min := conditions.Get(prefix + "AtLeast"); min.Exists() && value < min.Uint() {
		return false
	}
	if max := conditions.Get(prefix + "AtMost"); max.Exists() && value > max.Uint() {
		return false
	}
	return true
}

func tokenValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func priceRate(value gjson.Result) *float64 {
	if !value.Exists() || value.Type != gjson.Number {
		return nil
	}
	result := value.Float()
	return &result
}
