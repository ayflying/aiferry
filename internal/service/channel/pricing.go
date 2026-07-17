package channel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
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
	SourceKind  string `json:"sourceKind"`
	SourceID    uint64 `json:"sourceId"`
	SourceName  string `json:"sourceName"`
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
