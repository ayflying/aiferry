package usage

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/timewindow"
	"github.com/yunloli/aiferry/internal/model/entity"
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

func EstimatePublicModelCost(ctx context.Context, modelName, endpoint string, tokens TokenUsage, at time.Time) *decimal.Decimal {
	breakdown := EstimatePublicModelBreakdown(ctx, modelName, endpoint, tokens, at)
	if breakdown == nil {
		return nil
	}
	cost := breakdown.Cost()
	return &cost
}

func EstimatePublicModelBreakdown(ctx context.Context, modelName, endpoint string, tokens TokenUsage, at time.Time) *BillingBreakdown {
	var price publicModelPrice
	if err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Scan(&price); err != nil || price.PublicName == "" {
		return nil
	}
	switch price.BillingMode {
	case "rules":
		return EstimateRuleBreakdown(ctx, modelName, endpoint, tokens, at)
	case "request":
		breakdown := EstimateBreakdown(tokens, PriceRates{Request: price.RequestPrice})
		if breakdown != nil {
			breakdown.BillingMode = "request"
		}
		return breakdown
	default:
		return EstimateBreakdown(tokens, PriceRates{
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

func EstimateRuleCost(ctx context.Context, modelName, endpoint string, tokens TokenUsage, at time.Time) *decimal.Decimal {
	breakdown := EstimateRuleBreakdown(ctx, modelName, endpoint, tokens, at)
	if breakdown == nil {
		return nil
	}
	cost := breakdown.Cost()
	return &cost
}

// EstimateRuleBreakdown 按 priority 降序试匹配模型下的价格规则，命中第一条即返回。
// at 是用于时段判断的评估时刻，必须传「请求实际发生的时刻」，
// 否则历史用量重建会套用当前时间段的费率，与库中已记录金额对不上。
func EstimateRuleBreakdown(ctx context.Context, modelName, endpoint string, tokens TokenUsage, at time.Time) *BillingBreakdown {
	rules := make([]entity.ModelPriceRules, 0)
	columns := dao.ModelPriceRules.Columns()
	err := dao.ModelPriceRules.Ctx(ctx).
		Where(columns.ModelName, modelName).
		Where(columns.Status, 1).
		Scan(&rules)
	if err != nil {
		return nil
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		leftManual := rules[i].Source == "manual"
		rightManual := rules[j].Source == "manual"
		if leftManual != rightManual {
			return leftManual
		}
		return rules[i].Id > rules[j].Id
	})
	for _, rule := range rules {
		if breakdown, ok := RuleBreakdown(rule.ConditionsJson, rule.RatesJson, endpoint, tokens, at); ok {
			currency := strings.ToUpper(strings.TrimSpace(rule.Currency))
			if currency == "" {
				currency = "USD"
			}
			breakdown.BillingMode = "rules"
			breakdown.Currency = currency
			breakdown.Rule = &BillingRuleSnapshot{
				ID: rule.Id, Name: rule.Name, Source: rule.Source, Priority: rule.Priority, Conditions: rule.ConditionsJson,
			}
			return breakdown
		}
	}
	return nil
}

// RuleMatches 判断价格规则的条件是否命中本次请求。
// 支持三个维度：endpoint、token 区间、生效时段；未声明的维度一律视为不限。
func RuleMatches(conditionsJSON, endpoint string, tokens TokenUsage, at time.Time) bool {
	conditions := gjson.Parse(conditionsJSON)
	if configured := strings.TrimSpace(conditions.Get("endpoint").String()); configured != "" && configured != endpoint {
		return false
	}
	input, output := tokenValue(tokens.Input), tokenValue(tokens.Output)
	if !matchesTokenRange(conditions, "inputTokens", input) || !matchesTokenRange(conditions, "outputTokens", output) || !matchesTokenRange(conditions, "totalTokens", input+output) {
		return false
	}
	return matchesTimeCondition(conditions, at)
}

// matchesTimeCondition 判断 conditions.time 块是否命中给定时刻。结构：
//
//	{
//	  "tz": "Asia/Shanghai",
//	  "weekdays": [1, 2, 3, 4, 5],
//	  "ranges": [["09:00", "12:00"], ["14:00", "18:00"]]
//	}
//
// weekdays 用 ISO 8601 编号（1=周一 … 7=周日）；ranges 为 "HH:MM"，
// 支持跨零点区间（如 ["22:00","06:00"]）。三个字段都可省略：省略 weekdays
// 表示不限星期，省略 ranges 表示不限时刻。完全没有 time 块表示该规则
// 不受时间限制，永远命中；只写 tz 或七个星期全选同样等价于不限制。
//
// 解析与判定统一走 timewindow 包，避免时限语义在计费端和渠道关闭时段那边
// 各写一份后漂移（两处的时区、星期编号、跨零点规则必须完全一致）。
func matchesTimeCondition(conditions gjson.Result, at time.Time) bool {
	block := conditions.Get("time")
	if !block.Exists() {
		return true
	}
	window, err := timewindow.Parse(block.Raw)
	if err != nil {
		// 条件非法（时区名不认识、时段不是补零的 HH:MM）时不猜测语义，直接判不命中。
		// 反过来默认命中会把「高峰高价」这类条件规则退化成全天生效的兜底规则，
		// 那正是这次要修的问题；渠道侧 validateConditions 已在保存时拦截这类写法。
		return false
	}
	return window.Allows(at)
}

func RuleCost(conditionsJSON, ratesJSON, endpoint string, tokens TokenUsage, at time.Time) (*decimal.Decimal, bool) {
	if !RuleMatches(conditionsJSON, endpoint, tokens, at) {
		return nil, false
	}
	breakdown := EstimateBreakdown(tokens, rulePriceRates(ratesJSON))
	if breakdown == nil {
		return nil, false
	}
	cost := breakdown.Cost()
	return &cost, true
}

func RuleBreakdown(conditionsJSON, ratesJSON, endpoint string, tokens TokenUsage, at time.Time) (*BillingBreakdown, bool) {
	if !RuleMatches(conditionsJSON, endpoint, tokens, at) {
		return nil, false
	}
	breakdown := EstimateBreakdown(tokens, rulePriceRates(ratesJSON))
	return breakdown, breakdown != nil
}

func rulePriceRates(ratesJSON string) PriceRates {
	rates := gjson.Parse(ratesJSON)
	return PriceRates{
		Input:       priceRate(rates.Get("inputPerMillion")),
		CachedInput: priceRate(rates.Get("cachedInputPerMillion")),
		CacheWrite:  priceRate(rates.Get("cacheWritePerMillion")),
		Output:      priceRate(rates.Get("outputPerMillion")),
		ImageInput:  priceRate(rates.Get("imageInputPerMillion")),
		AudioInput:  priceRate(rates.Get("audioInputPerMillion")),
		AudioOutput: priceRate(rates.Get("audioOutputPerMillion")),
		Request:     priceRate(rates.Get("request")),
	}
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
