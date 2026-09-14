package channel

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// matchModelsForRule 把上游价格条目匹配到本地模型。先按完整名称精确匹配
// （UpstreamName 或 PublicName）；未命中且名称带厂商前缀（如
// z-ai/glm-5.3-flash）时，退回用最后一个 / 之后的后缀匹配。上游价格源
// 普遍带 org/ 前缀而本地模型名通常没有，精确匹配失败时按后缀兜底即可
// 命中 glm-5.3-flash 这类本地模型。
func matchModelsForRule(byName map[string][]entity.ChannelModels, ruleModel string) []entity.ChannelModels {
	if models, exists := byName[ruleModel]; exists {
		return models
	}
	if slash := strings.LastIndex(ruleModel, "/"); slash >= 0 {
		if suffix := strings.TrimSpace(ruleModel[slash+1:]); suffix != "" {
			return byName[suffix]
		}
	}
	return nil
}

func (s *sChannel) syncPricesFromPayload(ctx context.Context, endpoint string, config channeltype.PricingConfig, body []byte) (int, error) {
	rules, err := syncedRulesFromJSON(body, config)
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, gerror.New("upstream price query did not return price rules")
	}
	return s.saveSyncedPriceRules(ctx, endpoint, rules)
}

func (s *sChannel) saveSyncedPriceRules(ctx context.Context, endpoint string, rules []syncedRule) (int, error) {
	var models []entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Scan(&models); err != nil {
		return 0, gerror.Wrap(err, "load public models for prices")
	}
	byName := make(map[string][]entity.ChannelModels, len(models)*2)
	for _, model := range models {
		byName[model.UpstreamName] = append(byName[model.UpstreamName], model)
		byName[model.PublicName] = append(byName[model.PublicName], model)
	}
	publicRules := make(map[string][]syncedRule)
	canonicalModelIDs := make(map[string]uint64)
	for _, rule := range rules {
		seen := make(map[string]struct{})
		for _, model := range matchModelsForRule(byName, rule.Model) {
			if _, exists := seen[model.PublicName]; exists {
				continue
			}
			seen[model.PublicName] = struct{}{}
			publicRules[model.PublicName] = append(publicRules[model.PublicName], rule)
			if canonicalModelIDs[model.PublicName] == 0 || model.Id < canonicalModelIDs[model.PublicName] {
				canonicalModelIDs[model.PublicName] = model.Id
			}
		}
	}
	count := 0
	err := dao.ModelPriceRules.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		for modelName, modelRules := range publicRules {
			if _, deleteErr := dao.ModelPriceRules.Ctx(txCtx).Where(do.ModelPriceRules{ModelName: modelName, Source: "sync"}).Delete(); deleteErr != nil {
				return gerror.Wrap(deleteErr, "replace synced price rules")
			}
			conditional := false
			for _, rule := range orderSyncedRules(modelRules) {
				if ruleHasConditions(rule) {
					conditional = true
				}
				if _, insertErr := dao.ModelPriceRules.Ctx(txCtx).Data(do.ModelPriceRules{ChannelModelId: canonicalModelIDs[modelName], ModelName: modelName, Name: rule.Name, Source: "sync", SourceRef: endpoint, Currency: rule.Currency, ConditionsJson: string(rule.Conditions), RatesJson: string(rule.Rates), Status: 1, SyncedAt: gtime.Now()}).Insert(); insertErr != nil {
					return gerror.Wrap(insertErr, "save synced price rule")
				}
				count++
				if values, ok := modelPriceValuesFromRule(rule); ok {
					if saveErr := s.mergePublicPrice(txCtx, modelName, values); saveErr != nil {
						return saveErr
					}
				}
			}
			// 分档模型（峰谷/上下文阶梯）只有走规则计费才能命中对应档位，
			// 因此这里把它的公共价格切到 rules 模式；无条件模型不受影响。
			if conditional {
				if modeErr := s.setPublicBillingMode(txCtx, modelName, BillingModeRules); modeErr != nil {
					return modeErr
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err = s.prices.Load(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

func syncedRulesFromJSON(body []byte, config channeltype.PricingConfig) ([]syncedRule, error) {
	if config.Adapter == channeltype.AdapterNewAPIRatio {
		return syncedRulesFromNewAPIRatio(body)
	}
	items := gjson.ParseBytes(body)
	if config.ListPath != "" {
		items = gjson.GetBytes(body, config.ListPath)
	}
	if !items.IsArray() {
		return nil, gerror.New("price list path did not resolve to an array")
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
	return result, nil
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
func syncedRulesFromNewAPIRatio(body []byte) ([]syncedRule, error) {
	data := gjson.GetBytes(body, "data")
	if !data.IsObject() {
		return nil, gerror.New("NewAPI ratio source did not return a data object")
	}
	if expressions := data.Get("billing_expr"); expressions.IsObject() {
		return syncedRulesFromBillingExpressions(expressions, data.Get("billing_mode"))
	}
	modelRatios := newAPIRatioValues(data.Get("model_ratio"))
	if len(modelRatios) == 0 {
		return nil, gerror.New("NewAPI ratio source did not return model_ratio")
	}
	modelPrices := newAPIRatioValues(data.Get("model_price"))
	cacheRatios := newAPIRatioValues(data.Get("cache_ratio"))
	completionRatios := newAPIRatioValues(data.Get("completion_ratio"))
	rules := make([]syncedRule, 0, len(modelRatios)+len(modelPrices))
	for model, ratio := range modelRatios {
		if _, usesRequestPrice := modelPrices[model]; usesRequestPrice {
			continue
		}
		input := ratio * newAPIRatioUSDPerMillion
		rates := map[string]float64{"inputPerMillion": input, "outputPerMillion": input * completionRatio(completionRatios, model)}
		if cacheRatio, exists := cacheRatios[model]; exists {
			rates["cachedInputPerMillion"] = input * cacheRatio
		}
		encoded, _ := json.Marshal(rates)
		rules = append(rules, syncedRule{Model: model, Name: "BaseLLM 官方模型价格", Currency: "USD", Conditions: json.RawMessage(`{}`), Rates: encoded})
	}
	for model, price := range modelPrices {
		encoded, _ := json.Marshal(map[string]float64{"request": price})
		rules = append(rules, syncedRule{Model: model, Name: "BaseLLM 官方按次价格", Currency: "USD", Conditions: json.RawMessage(`{}`), Rates: encoded})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Model < rules[j].Model })
	return rules, nil
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

// orderSyncedRules 让无条件规则先入库、条件规则后入库。规则匹配按 id 降序
// 逐条试错，后入库的条件规则（峰谷价、上下文阶梯）因此先于兜底规则命中。
func orderSyncedRules(rules []syncedRule) []syncedRule {
	ordered := make([]syncedRule, len(rules))
	copy(ordered, rules)
	sort.SliceStable(ordered, func(i, j int) bool {
		return !ruleHasConditions(ordered[i]) && ruleHasConditions(ordered[j])
	})
	return ordered
}

func ruleHasConditions(rule syncedRule) bool {
	conditions := strings.TrimSpace(string(rule.Conditions))
	return conditions != "" && conditions != "{}" && conditions != "null"
}

// setPublicBillingMode 切换公共价格的计费模式。分档模型（上下文阶梯等）只有
// 走 rules 计费才能命中对应档位，否则规则虽然入库却不会参与计费。
// 记录不存在时创建，避免规则已写入却缺少计费入口。
func (s *sChannel) setPublicBillingMode(ctx context.Context, modelName, mode string) error {
	var current PublicModelView
	if err := dao.ModelPrices.Ctx(ctx).
		Where(dao.ModelPrices.Columns().PublicName, modelName).
		Scan(&current); err != nil && !isMissingPublicPriceError(err) {
		return gerror.Wrap(err, "load current public model price mode")
	}
	if current.PublicName == "" {
		if _, err := dao.ModelPrices.Ctx(ctx).Data(do.ModelPrices{PublicName: modelName, BillingMode: mode}).Insert(); err != nil {
			return gerror.Wrap(err, "create public model price for rules billing")
		}
		return nil
	}
	if current.BillingMode == mode {
		return nil
	}
	if _, err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Data(do.ModelPrices{BillingMode: mode}).Update(); err != nil {
		return gerror.Wrap(err, "switch public model price to rules billing")
	}
	return nil
}
