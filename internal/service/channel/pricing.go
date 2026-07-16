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

type syncedRule struct {
	Model      string
	Name       string
	Currency   string
	Conditions json.RawMessage
	Rates      json.RawMessage
}

func (s *Service) ListPriceRules(ctx context.Context, modelID uint64) ([]PriceRuleView, error) {
	rows := make([]entity.ModelPriceRules, 0)
	if err := dao.ModelPriceRules.Ctx(ctx).Where(dao.ModelPriceRules.Columns().ChannelModelId, modelID).OrderDesc(dao.ModelPriceRules.Columns().Priority).OrderDesc(dao.ModelPriceRules.Columns().Id).Scan(&rows); err != nil {
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
	if exists, err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Count(); err != nil || exists == 0 {
		if err != nil {
			return 0, gerror.Wrap(err, "find model for price rule")
		}
		return 0, gerror.New("model not found")
	}
	conditions := normalizeJSON(input.Conditions, []byte(`{}`))
	created, err := dao.ModelPriceRules.Ctx(ctx).Data(do.ModelPriceRules{
		ChannelModelId: modelID, Name: strings.TrimSpace(input.Name), Source: input.Source, SourceRef: strings.TrimSpace(input.SourceRef), Priority: input.Priority,
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

func (s *Service) SyncPrices(ctx context.Context, channelID uint64) (int, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return 0, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return 0, err
	}
	if config.Pricing.Adapter == channeltype.AdapterNone {
		return 0, gerror.New("channel type does not configure price synchronization")
	}
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
	if err = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().ChannelId, channelID).Scan(&models); err != nil {
		return 0, gerror.Wrap(err, "load channel models for prices")
	}
	byName := make(map[string]entity.ChannelModels, len(models))
	for _, model := range models {
		byName[model.UpstreamName] = model
		byName[model.PublicName] = model
	}
	count := 0
	err = dao.ModelPriceRules.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		seenModels := make(map[uint64]struct{}, len(models))
		for _, model := range models {
			if _, exists := seenModels[model.Id]; exists {
				continue
			}
			seenModels[model.Id] = struct{}{}
			if _, deleteErr := dao.ModelPriceRules.Ctx(txCtx).Where(do.ModelPriceRules{ChannelModelId: model.Id, Source: "sync"}).Delete(); deleteErr != nil {
				return gerror.Wrap(deleteErr, "replace synced price rules")
			}
		}
		for _, rule := range rules {
			model, ok := byName[rule.Model]
			if !ok {
				continue
			}
			if _, insertErr := dao.ModelPriceRules.Ctx(txCtx).Data(do.ModelPriceRules{ChannelModelId: model.Id, Name: rule.Name, Source: "sync", SourceRef: endpoint, Currency: rule.Currency, ConditionsJson: string(rule.Conditions), RatesJson: string(rule.Rates), Status: 1, SyncedAt: time.Now()}).Insert(); insertErr != nil {
				return gerror.Wrap(insertErr, "save synced price rule")
			}
			count++
		}
		return nil
	})
	return count, err
}

func syncedRulesFromJSON(body []byte, config channeltype.PricingConfig) ([]syncedRule, error) {
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
			if config.OutputPricePath != "" {
				flat["outputPerMillion"] = item.Get(config.OutputPricePath).Float()
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

func priceRuleView(row entity.ModelPriceRules) (PriceRuleView, error) {
	conditions := json.RawMessage(row.ConditionsJson)
	rates := json.RawMessage(row.RatesJson)
	if !json.Valid(conditions) || !json.Valid(rates) {
		return PriceRuleView{}, gerror.New("stored price rule has invalid JSON")
	}
	view := PriceRuleView{Id: row.Id, ChannelModelID: row.ChannelModelId, Name: row.Name, Source: row.Source, SourceRef: row.SourceRef, Priority: row.Priority, Currency: row.Currency, Conditions: conditions, Rates: rates, Status: row.Status, UpdatedAt: row.UpdatedAt}
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
