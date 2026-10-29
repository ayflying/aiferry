package channel

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// 本文件负责「上游载荷 → 同步规则」：适配 newapi_ratio 与通用 JSON 列表两种
// 价格源。计费表达式本身见 price_expr*.go，落库见 price_rules_sync.go。

// syncedRulesFromJSON 把上游响应解析成同步规则。
// 第二个返回值是「上游给出了条目但不可用」的跳过记录：它既不是解析失败（不该
// 阻断整次同步），也不能静默丢弃（管理员会以为已经覆盖全部模型）。
func syncedRulesFromJSON(body []byte, config channeltype.PricingConfig) ([]syncedRule, []PriceSyncSkip, error) {
	if config.Adapter == channeltype.AdapterNewAPIRatio {
		return syncedRulesFromNewAPIRatio(body)
	}
	items := gjson.ParseBytes(body)
	if config.ListPath != "" {
		items = gjson.GetBytes(body, config.ListPath)
	}
	if !items.IsArray() {
		return nil, nil, gerror.New("price list path did not resolve to an array")
	}
	result := make([]syncedRule, 0, len(items.Array()))
	for _, item := range items.Array() {
		model := strings.TrimSpace(item.Get(config.ModelPath).String())
		if model == "" {
			continue
		}
		rule := syncedRuleFromJSONItem(item, config, model)
		if len(rule.Rates) == 0 || string(rule.Rates) == "null" {
			continue
		}
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Model < result[j].Model })
	return result, nil, nil
}

func syncedRuleFromJSONItem(item gjson.Result, config channeltype.PricingConfig, model string) syncedRule {
	name := strings.TrimSpace(item.Get(config.NamePath).String())
	if name == "" {
		name = "同步价格"
	}
	currency := strings.ToUpper(strings.TrimSpace(item.Get(config.CurrencyPath).String()))
	if currency == "" {
		currency = "USD"
	}
	conditions := json.RawMessage(`{}`)
	if config.ConditionsPath != "" {
		conditions = normalizeJSON([]byte(item.Get(config.ConditionsPath).Raw), conditions)
	}
	return syncedRule{Model: model, Name: name, Currency: currency, Conditions: conditions, Rates: syncedRuleRates(item, config)}
}

func syncedRuleRates(item gjson.Result, config channeltype.PricingConfig) json.RawMessage {
	if config.RatesPath != "" {
		return normalizeJSON([]byte(item.Get(config.RatesPath).Raw), nil)
	}
	paths := []struct{ key, path string }{
		{"inputPerMillion", config.InputPricePath}, {"cachedInputPerMillion", config.CachedInputPricePath},
		{"cacheWritePerMillion", config.CacheWritePricePath}, {"outputPerMillion", config.OutputPricePath},
		{"imageInputPerMillion", config.ImageInputPricePath}, {"audioInputPerMillion", config.AudioInputPricePath},
		{"audioOutputPerMillion", config.AudioOutputPricePath}, {"request", config.RequestPricePath},
	}
	rates := make(map[string]float64)
	for _, value := range paths {
		if value.path != "" {
			rates[value.key] = item.Get(value.path).Float()
		}
	}
	if len(rates) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(rates)
	return encoded
}

// syncedRulesFromNewAPIRatio 兼容两种上游载荷：BaseLLM 自 2026-09 起改为
// billing_expr（tiered_expr 计费表达式），旧版本仍是 model_ratio 倍率表。
func syncedRulesFromNewAPIRatio(body []byte) ([]syncedRule, []PriceSyncSkip, error) {
	data := gjson.GetBytes(body, "data")
	if !data.IsObject() {
		return nil, nil, gerror.New("NewAPI ratio source did not return a data object")
	}
	if expressions := data.Get("billing_expr"); expressions.IsObject() {
		return syncedRulesFromBillingExpressions(expressions, data.Get("billing_mode"))
	}
	modelRatios := newAPIRatioValues(data.Get("model_ratio"))
	if len(modelRatios) == 0 {
		return nil, nil, gerror.New("NewAPI ratio source did not return model_ratio")
	}
	modelPrices := newAPIRatioValues(data.Get("model_price"))
	cacheRatios := newAPIRatioValues(data.Get("cache_ratio"))
	completionRatios := newAPIRatioValues(data.Get("completion_ratio"))
	rules := make([]syncedRule, 0, len(modelRatios)+len(modelPrices))
	skips := make([]PriceSyncSkip, 0)
	for model, ratio := range modelRatios {
		if _, usesRequestPrice := modelPrices[model]; usesRequestPrice {
			continue
		}
		input := ratio * newAPIRatioUSDPerMillion
		if input == 0 {
			// 倍率为 0 的条目同样是「不计费」，不能落成 0 价规则。
			skips = append(skips, PriceSyncSkip{Model: model, Reason: "上游未按量计费（倍率为 0，多为订阅制或免费额度），跳过以免按 0 计费"})
			continue
		}
		rates := map[string]float64{"inputPerMillion": input, "outputPerMillion": input * completionRatio(completionRatios, model)}
		if cacheRatio, exists := cacheRatios[model]; exists {
			rates["cachedInputPerMillion"] = input * cacheRatio
		}
		encoded, _ := json.Marshal(rates)
		rules = append(rules, syncedRule{Model: model, Name: billingExprRuleName, Currency: "USD", Conditions: json.RawMessage(`{}`), Rates: encoded})
	}
	for model, price := range modelPrices {
		if price == 0 {
			skips = append(skips, PriceSyncSkip{Model: model, Reason: "上游未按量计费（按次价格为 0，多为订阅制或免费额度），跳过以免按 0 计费"})
			continue
		}
		encoded, _ := json.Marshal(map[string]float64{"request": price})
		rules = append(rules, syncedRule{Model: model, Name: "BaseLLM 官方按次价格", Currency: "USD", Conditions: json.RawMessage(`{}`), Rates: encoded})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Model < rules[j].Model })
	return rules, skips, nil
}

func newAPIRatioValues(value gjson.Result) map[string]float64 {
	values := make(map[string]float64)
	if !value.IsObject() {
		return values
	}
	value.ForEach(func(key, item gjson.Result) bool {
		amount := item.Float()
		if model := strings.TrimSpace(key.String()); model != "" && item.Type == gjson.Number && amount >= 0 {
			values[model] = amount
		}
		return true
	})
	return values
}

func completionRatio(values map[string]float64, model string) float64 {
	if value, exists := values[model]; exists {
		return value
	}
	return 1
}
