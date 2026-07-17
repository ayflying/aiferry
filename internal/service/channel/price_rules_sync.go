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
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

func (s *Service) syncPricesFromPayload(ctx context.Context, endpoint string, config channeltype.PricingConfig, body []byte) (int, error) {
	rules, err := syncedRulesFromJSON(body, config)
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, gerror.New("upstream price query did not return price rules")
	}
	return s.saveSyncedPriceRules(ctx, endpoint, rules)
}

func (s *Service) saveSyncedPriceRules(ctx context.Context, endpoint string, rules []syncedRule) (int, error) {
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
		for _, model := range byName[rule.Model] {
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
			for _, rule := range modelRules {
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

func syncedRulesFromNewAPIRatio(body []byte) ([]syncedRule, error) {
	data := gjson.GetBytes(body, "data")
	if !data.IsObject() {
		return nil, gerror.New("NewAPI ratio source did not return a data object")
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
