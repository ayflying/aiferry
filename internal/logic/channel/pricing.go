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
	"github.com/yunloli/aiferry/internal/logic/timewindow"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 本文件负责价格规则（model_price_rules）的增删改查、同步结果结构与校验；
// 公共模型价格（model_prices）的写入见 price_public.go，
// 上游价格同步流程见 price_sync.go / price_rules_sync.go / price_payload.go。

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
	Count        int                      `json:"count"`
	Sources      int                      `json:"sources"`
	Succeeded    int                      `json:"succeeded"`
	SkippedCount int                      `json:"skippedCount"`
	Skipped      []PriceSyncSkip          `json:"skipped"`
	Failures     []PriceSyncSourceFailure `json:"failures"`
}

// PriceSyncSkip 是一个因上游数据不可用而没写入价格的模型。
// 它不是失败：上游确实没给出可用的价格（典型是订阅制模型系数全为 0），
// 但管理员需要看到它，否则会以为同步已经覆盖了全部模型。
type PriceSyncSkip struct {
	Source string `json:"source,omitempty"`
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

func newPriceSyncResult() PriceSyncResult {
	return PriceSyncResult{
		Skipped:  make([]PriceSyncSkip, 0),
		Failures: make([]PriceSyncSourceFailure, 0),
	}
}

// add 把单个价格源的同步结果并进总结果，并给跳过记录补上来源名，
// 让管理端能看出是哪个价格源跳过了哪些模型。
func (result *PriceSyncResult) add(stats priceSyncStats, sourceName string) {
	result.Count += stats.Count
	for _, skip := range stats.Skipped {
		skip.Source = sourceName
		result.Skipped = append(result.Skipped, skip)
	}
	result.SkippedCount = len(result.Skipped)
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

func (s *sChannel) ListPriceRules(ctx context.Context, modelID uint64) ([]PriceRuleView, error) {
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

func (s *sChannel) CreatePriceRule(ctx context.Context, modelID uint64, input adminapi.PriceRuleInput) (uint64, error) {
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
	if err != nil {
		return 0, gerror.Wrap(err, "create model price rule")
	}
	if err = s.prices.Load(ctx); err != nil {
		return 0, err
	}
	return uint64(created), nil
}

func (s *sChannel) UpdatePriceRule(ctx context.Context, id uint64, input adminapi.PriceRuleInput) error {
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
	return s.prices.Load(ctx)
}

func (s *sChannel) DeletePriceRule(ctx context.Context, id uint64) error {
	result, err := dao.ModelPriceRules.Ctx(ctx).Where(dao.ModelPriceRules.Columns().Id, id).Delete()
	if err != nil {
		return gerror.Wrap(err, "delete model price rule")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("price rule not found")
	}
	return s.prices.Load(ctx)
}

func (s *sChannel) publicModelName(ctx context.Context, modelID uint64) (string, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Scan(&model); err != nil {
		return "", gerror.Wrap(err, "find model for price rule")
	}
	if model.Id == 0 {
		return "", gerror.New("model not found")
	}
	return model.PublicName, nil
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
	return validateConditions(input.Conditions)
}

// validateConditions 校验 conditions.time 块，让非法时区/星期/时段在保存时就被拒绝，
// 而不是等到计费时静默不命中（那时用户只会看到「未命中规则、无法计费」）。
//
// 校验直接复用 timewindow.Parse：计费端判定用的是同一个解析器，
// 两边共用一份规则，不会出现「保存通过、运行时判不命中」的写法差异
// （时段必须是补零的 HH:MM，全天用起止相同表示，跨零点用 start > end 表示）。
func validateConditions(raw json.RawMessage) error {
	block := gjson.ParseBytes(raw).Get("time")
	if !block.Exists() {
		return nil
	}
	if _, err := timewindow.Parse(block.Raw); err != nil {
		return gerror.Wrap(err, "price rule conditions.time is invalid")
	}
	return nil
}

func normalizeJSON(raw, fallback []byte) json.RawMessage {
	if len(raw) > 0 && json.Valid(raw) && string(raw) != "null" {
		return append(json.RawMessage(nil), raw...)
	}
	return append(json.RawMessage(nil), fallback...)
}
