package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

type PriceRuleView struct {
	Id             uint64          `json:"id"`
	ChannelModelID uint64          `json:"channelModelId"`
	ModelName      string          `json:"modelName"`
	Name           string          `json:"name"`
	Source         string          `json:"source"`
	SourceRef      string          `json:"sourceRef"`
	Priority       int             `json:"priority"`
	Currency       string          `json:"currency"`
	Conditions     json.RawMessage `json:"conditions"`
	Rates          json.RawMessage `json:"rates"`
	Status         int             `json:"status"`
	SyncedAt       *time.Time      `json:"syncedAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type PriceSyncResult struct {
	Count     int                      `json:"count"`
	Sources   int                      `json:"sources"`
	Succeeded int                      `json:"succeeded"`
	Failures  []PriceSyncSourceFailure `json:"failures"`
}

type PriceSyncSourceFailure struct {
	ChannelID   uint64 `json:"channelId"`
	ChannelName string `json:"channelName"`
	Message     string `json:"message"`
}

type modelPriceValues struct {
	Input       *float64
	CachedInput *float64
	CacheWrite  *float64
	Output      *float64
	ImageInput  *float64
	AudioInput  *float64
	AudioOutput *float64
	Request     *float64
}

type syncedRule struct {
	Model      string
	Name       string
	Currency   string
	Conditions json.RawMessage
	Rates      json.RawMessage
}

const newAPIRatioUSDPerMillion = 2.0

const (
	BillingModeToken   = "token"
	BillingModeRequest = "request"
	BillingModeRules   = "rules"
)

func (s *Service) ListPriceRules(ctx context.Context, modelID uint64) ([]PriceRuleView, error) {
	modelName, err := s.publicModelName(ctx, modelID)
	if err != nil {
		return nil, err
	}
	rows := make([]entity.ModelPriceRules, 0)
	if err = dao.ModelPriceRules.Ctx(ctx).Where(dao.ModelPriceRules.Columns().ModelName, modelName).OrderDesc(dao.ModelPriceRules.Columns().Priority).OrderDesc(dao.ModelPriceRules.Columns().Id).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list model price rules")
	}
	views := make([]PriceRuleView, 0, len(rows))
	for _, row := range rows {
		view, err := priceRuleView(row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) CreatePriceRule(ctx context.Context, modelID uint64, input adminapi.PriceRuleInput) (uint64, error) {
	if err := validatePriceRule(input); err != nil {
		return 0, err
	}
	modelName, err := s.publicModelName(ctx, modelID)
	if err != nil {
		return 0, err
	}
	conditions := normalizeJSON(input.Conditions, []byte(`{}`))
	created, err := dao.ModelPriceRules.Ctx(ctx).Data(do.ModelPriceRules{
		ChannelModelId: modelID, ModelName: modelName, Name: strings.TrimSpace(input.Name), Source: input.Source, SourceRef: strings.TrimSpace(input.SourceRef), Priority: input.Priority,
		Currency: strings.ToUpper(strings.TrimSpace(input.Currency)), ConditionsJson: string(conditions), RatesJson: string(input.Rates), Status: boolStatus(input.Status),
	}).InsertAndGetId()
	return uint64(created), gerror.Wrap(err, "create model price rule")
}

func (s *Service) UpdatePriceRule(ctx context.Context, id uint64, input adminapi.PriceRuleInput) error {
	if err := validatePriceRule(input); err != nil {
		return err
	}
	data := do.ModelPriceRules{Name: strings.TrimSpace(input.Name), SourceRef: strings.TrimSpace(input.SourceRef), Priority: input.Priority, Currency: strings.ToUpper(strings.TrimSpace(input.Currency)), ConditionsJson: string(normalizeJSON(input.Conditions, []byte(`{}`))), RatesJson: string(input.Rates), Status: boolStatus(input.Status)}
	result, err := dao.ModelPriceRules.Ctx(ctx).Where(dao.ModelPriceRules.Columns().Id, id).Data(data).Update()
	if err != nil {
		return gerror.Wrap(err, "update model price rule")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("price rule not found")
	}
	return nil
}

func (s *Service) DeletePriceRule(ctx context.Context, id uint64) error {
	result, err := dao.ModelPriceRules.Ctx(ctx).Where(dao.ModelPriceRules.Columns().Id, id).Delete()
	if err != nil {
		return gerror.Wrap(err, "delete model price rule")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("price rule not found")
	}
	return nil
}

func (s *Service) SyncAllPrices(ctx context.Context) (PriceSyncResult, error) {
	var channels []entity.Channels
	if err := dao.Channels.Ctx(ctx).
		Where(dao.Channels.Columns().Status, 1).
		OrderAsc(dao.Channels.Columns().Priority).
		OrderAsc(dao.Channels.Columns().Id).
		Scan(&channels); err != nil {
		return PriceSyncResult{}, gerror.Wrap(err, "list price sync channels")
	}

	result := PriceSyncResult{Failures: make([]PriceSyncSourceFailure, 0)}
	for _, channel := range channels {
		_, config, err := s.types.GetByCode(ctx, channel.Type)
		if err != nil {
			result.Sources++
			result.Failures = append(result.Failures, PriceSyncSourceFailure{
				ChannelID:   channel.Id,
				ChannelName: channel.Name,
				Message:     err.Error(),
			})
			continue
		}
		if config.Pricing.Adapter == channeltype.AdapterNone {
			continue
		}
		result.Sources++
		count, err := s.syncPricesFromChannel(ctx, channel, config)
		if err != nil {
			result.Failures = append(result.Failures, PriceSyncSourceFailure{
				ChannelID:   channel.Id,
				ChannelName: channel.Name,
				Message:     err.Error(),
			})
			continue
		}
		result.Count += count
		result.Succeeded++
	}
	return result, nil
}

func (s *Service) SyncPriceSource(ctx context.Context, channelID uint64) (PriceSyncResult, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return PriceSyncResult{}, err
	}
	result := PriceSyncResult{Sources: 1, Failures: make([]PriceSyncSourceFailure, 0)}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		result.Failures = append(result.Failures, PriceSyncSourceFailure{ChannelID: channel.Id, ChannelName: channel.Name, Message: err.Error()})
		return result, nil
	}
	if config.Pricing.Adapter == channeltype.AdapterNone {
		result.Failures = append(result.Failures, PriceSyncSourceFailure{ChannelID: channel.Id, ChannelName: channel.Name, Message: "渠道类型没有配置价格同步接口"})
		return result, nil
	}
	count, err := s.syncPricesFromChannel(ctx, channel, config)
	if err != nil {
		result.Failures = append(result.Failures, PriceSyncSourceFailure{ChannelID: channel.Id, ChannelName: channel.Name, Message: err.Error()})
		return result, nil
	}
	result.Count = count
	result.Succeeded = 1
	return result, nil
}

func (s *Service) syncPricesFromChannel(ctx context.Context, channel entity.Channels, config channeltype.Config) (int, error) {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Pricing.Path)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, config.Pricing.Method, endpoint, nil)
	if err != nil {
		return 0, gerror.Wrap(err, "create price sync request")
	}
	if err = s.setConfiguredHeaders(ctx, req, channel, config.Pricing.AuthType, config.Pricing.HeaderName, config.Pricing.HeaderPrefix); err != nil {
		return 0, err
	}
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		return 0, gerror.Wrap(err, "fetch upstream prices")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, gerror.Wrap(err, "read upstream prices")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, gerror.Newf("upstream price query returned HTTP %d", resp.StatusCode)
	}
	if !gjson.ValidBytes(body) {
		return 0, gerror.New("upstream price query returned invalid JSON")
	}
	rules, err := syncedRulesFromJSON(body, config.Pricing)
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, gerror.New("upstream price query did not return price rules")
	}

	var models []entity.ChannelModels
	if err = dao.ChannelModels.Ctx(ctx).Scan(&models); err != nil {
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
	err = dao.ModelPriceRules.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
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
	return count, err
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
		rates := json.RawMessage(nil)
		if config.RatesPath != "" {
			rates = normalizeJSON([]byte(item.Get(config.RatesPath).Raw), nil)
		}
		if len(rates) == 0 {
			flat := map[string]float64{}
			if config.InputPricePath != "" {
				flat["inputPerMillion"] = item.Get(config.InputPricePath).Float()
			}
			if config.CachedInputPricePath != "" {
				flat["cachedInputPerMillion"] = item.Get(config.CachedInputPricePath).Float()
			}
			if config.CacheWritePricePath != "" {
				flat["cacheWritePerMillion"] = item.Get(config.CacheWritePricePath).Float()
			}
			if config.OutputPricePath != "" {
				flat["outputPerMillion"] = item.Get(config.OutputPricePath).Float()
			}
			if config.ImageInputPricePath != "" {
				flat["imageInputPerMillion"] = item.Get(config.ImageInputPricePath).Float()
			}
			if config.AudioInputPricePath != "" {
				flat["audioInputPerMillion"] = item.Get(config.AudioInputPricePath).Float()
			}
			if config.AudioOutputPricePath != "" {
				flat["audioOutputPerMillion"] = item.Get(config.AudioOutputPricePath).Float()
			}
			if config.RequestPricePath != "" {
				flat["request"] = item.Get(config.RequestPricePath).Float()
			}
			if len(flat) > 0 {
				encoded, _ := json.Marshal(flat)
				rates = encoded
			}
		}
		if len(rates) == 0 || string(rates) == "null" {
			continue
		}
		result = append(result, syncedRule{Model: model, Name: name, Currency: currency, Conditions: conditions, Rates: rates})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Model < result[j].Model })
	return result, nil
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
		rates := map[string]float64{
			"inputPerMillion":  input,
			"outputPerMillion": input * completionRatio(completionRatios, model),
		}
		if cacheRatio, exists := cacheRatios[model]; exists {
			rates["cachedInputPerMillion"] = input * cacheRatio
		}
		encoded, _ := json.Marshal(rates)
		rules = append(rules, syncedRule{
			Model:      model,
			Name:       "BaseLLM 官方模型价格",
			Currency:   "USD",
			Conditions: json.RawMessage(`{}`),
			Rates:      encoded,
		})
	}
	for model, price := range modelPrices {
		encoded, _ := json.Marshal(map[string]float64{"request": price})
		rules = append(rules, syncedRule{
			Model:      model,
			Name:       "BaseLLM 官方按次价格",
			Currency:   "USD",
			Conditions: json.RawMessage(`{}`),
			Rates:      encoded,
		})
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

func (s *Service) publicModelName(ctx context.Context, modelID uint64) (string, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Scan(&model); err != nil {
		return "", gerror.Wrap(err, "find model for price rule")
	}
	if model.Id == 0 {
		return "", gerror.New("model not found")
	}
	return model.PublicName, nil
}

func (s *Service) replacePublicPrice(ctx context.Context, modelName string, values modelPriceValues) error {
	return s.savePublicPrice(ctx, modelName, values, true, false, BillingModeToken)
}

func (s *Service) mergePublicPrice(ctx context.Context, modelName string, values modelPriceValues) error {
	return s.savePublicPrice(ctx, modelName, values, false, false, "")
}

func (s *Service) savePublicPrice(ctx context.Context, modelName string, values modelPriceValues, replaceToken, replaceRequest bool, billingMode string) error {
	data := do.ModelPrices{PublicName: modelName}
	if billingMode != "" {
		data.BillingMode = billingMode
	}
	if replaceToken || values.Input != nil {
		data.InputPrice = nullableNumber(values.Input)
	}
	if replaceToken || values.CachedInput != nil {
		data.CachedInputPrice = nullableNumber(values.CachedInput)
	}
	if replaceToken || values.CacheWrite != nil {
		data.CacheWritePrice = nullableNumber(values.CacheWrite)
	}
	if replaceToken || values.Output != nil {
		data.OutputPrice = nullableNumber(values.Output)
	}
	if replaceToken || values.ImageInput != nil {
		data.ImageInputPrice = nullableNumber(values.ImageInput)
	}
	if replaceToken || values.AudioInput != nil {
		data.AudioInputPrice = nullableNumber(values.AudioInput)
	}
	if replaceToken || values.AudioOutput != nil {
		data.AudioOutputPrice = nullableNumber(values.AudioOutput)
	}
	if replaceRequest || values.Request != nil {
		data.RequestPrice = nullableNumber(values.Request)
	}
	result, err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Data(data).Update()
	if err != nil {
		return gerror.Wrap(err, "update public model price")
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	if _, err = dao.ModelPrices.Ctx(ctx).Data(data).Insert(); err != nil {
		return gerror.Wrap(err, "create public model price")
	}
	return nil
}

func (s *Service) updatePublicModelPrice(ctx context.Context, modelName string, input adminapi.ModelPriceInput) error {
	mode, err := normalizeBillingMode(input.BillingMode)
	if err != nil {
		return err
	}
	values := modelPriceValues{
		Input:       input.InputPrice,
		CachedInput: input.CachedInputPrice,
		CacheWrite:  input.CacheWritePrice,
		Output:      input.OutputPrice,
		ImageInput:  input.ImageInputPrice,
		AudioInput:  input.AudioInputPrice,
		AudioOutput: input.AudioOutputPrice,
		Request:     input.RequestPrice,
	}
	switch mode {
	case BillingModeToken:
		return s.savePublicPrice(ctx, modelName, values, true, false, mode)
	case BillingModeRequest:
		return s.savePublicPrice(ctx, modelName, values, false, true, mode)
	default:
		return s.savePublicPrice(ctx, modelName, values, false, false, mode)
	}
}

func normalizeBillingMode(value string) (string, error) {
	switch value = strings.TrimSpace(value); value {
	case "", BillingModeToken:
		return BillingModeToken, nil
	case BillingModeRequest, BillingModeRules:
		return value, nil
	default:
		return "", gerror.New("unsupported model billing mode")
	}
}

func modelPriceValuesFromRule(rule syncedRule) (modelPriceValues, bool) {
	if conditions := strings.TrimSpace(string(rule.Conditions)); conditions != "" && conditions != "{}" {
		return modelPriceValues{}, false
	}
	rates := gjson.ParseBytes(rule.Rates)
	values := modelPriceValues{
		Input:       jsonPriceValue(rates.Get("inputPerMillion")),
		CachedInput: jsonPriceValue(rates.Get("cachedInputPerMillion")),
		CacheWrite:  jsonPriceValue(rates.Get("cacheWritePerMillion")),
		Output:      jsonPriceValue(rates.Get("outputPerMillion")),
		ImageInput:  jsonPriceValue(rates.Get("imageInputPerMillion")),
		AudioInput:  jsonPriceValue(rates.Get("audioInputPerMillion")),
		AudioOutput: jsonPriceValue(rates.Get("audioOutputPerMillion")),
		Request:     jsonPriceValue(rates.Get("request")),
	}
	return values, values.Input != nil || values.CachedInput != nil || values.CacheWrite != nil || values.Output != nil || values.ImageInput != nil || values.AudioInput != nil || values.AudioOutput != nil || values.Request != nil
}

func jsonPriceValue(value gjson.Result) *float64 {
	if !value.Exists() {
		return nil
	}
	result := value.Float()
	return &result
}

func priceRuleView(row entity.ModelPriceRules) (PriceRuleView, error) {
	conditions := json.RawMessage(row.ConditionsJson)
	rates := json.RawMessage(row.RatesJson)
	if !json.Valid(conditions) || !json.Valid(rates) {
		return PriceRuleView{}, gerror.New("stored price rule has invalid JSON")
	}
	view := PriceRuleView{Id: row.Id, ChannelModelID: row.ChannelModelId, ModelName: row.ModelName, Name: row.Name, Source: row.Source, SourceRef: row.SourceRef, Priority: row.Priority, Currency: row.Currency, Conditions: conditions, Rates: rates, Status: row.Status}
	if !row.UpdatedAt.IsZero() {
		view.UpdatedAt = row.UpdatedAt
	}
	if !row.SyncedAt.IsZero() {
		value := row.SyncedAt
		view.SyncedAt = &value
	}
	return view, nil
}

func validatePriceRule(input adminapi.PriceRuleInput) error {
	if input.Source != "manual" && input.Source != "sync" {
		return gerror.New("unsupported price rule source")
	}
	if !json.Valid(input.Rates) {
		return gerror.New("price rule rates must be valid JSON")
	}
	if len(input.Conditions) > 0 && !json.Valid(input.Conditions) {
		return gerror.New("price rule conditions must be valid JSON")
	}
	return nil
}

func normalizeJSON(raw, fallback []byte) json.RawMessage {
	if len(raw) > 0 && json.Valid(raw) && string(raw) != "null" {
		return append(json.RawMessage(nil), raw...)
	}
	return append(json.RawMessage(nil), fallback...)
}
