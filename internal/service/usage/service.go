package usage

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

type Service struct{}

type TokenUsage struct {
	Input       *uint64 `json:"input"`
	CachedInput *uint64 `json:"cachedInput"`
	CacheWrite  *uint64 `json:"cacheWrite"`
	ImageInput  *uint64 `json:"imageInput"`
	AudioInput  *uint64 `json:"audioInput"`
	Output      *uint64 `json:"output"`
	AudioOutput *uint64 `json:"audioOutput"`
	Total       *uint64 `json:"total"`
}

type PriceRates struct {
	Input       *float64
	CachedInput *float64
	CacheWrite  *float64
	Output      *float64
	ImageInput  *float64
	AudioInput  *float64
	AudioOutput *float64
	Request     *float64
}

type RecordInput struct {
	RequestID      string
	UserID         uint64
	APIKeyID       uint64
	ChannelID      uint64
	Endpoint       string
	RequestedModel string
	UpstreamModel  string
	HTTPStatus     int
	Stream         bool
	Tokens         TokenUsage
	EstimatedCost  *decimal.Decimal
	DurationMs     int64
	FirstTokenMs   *int64
	Attempts       int
	ErrorMessage   string
}

type Summary struct {
	Requests       int64    `json:"requests" orm:"requests"`
	Successes      int64    `json:"successes" orm:"successes"`
	InputTokens    uint64   `json:"inputTokens" orm:"input_tokens"`
	OutputTokens   uint64   `json:"outputTokens" orm:"output_tokens"`
	TotalTokens    uint64   `json:"totalTokens" orm:"total_tokens"`
	EstimatedCost  *float64 `json:"estimatedCost" orm:"estimated_cost"`
	AverageLatency float64  `json:"averageLatency" orm:"average_latency"`
}

type TrendPoint struct {
	Bucket        string   `json:"bucket" orm:"bucket"`
	Requests      int64    `json:"requests" orm:"requests"`
	InputTokens   uint64   `json:"inputTokens" orm:"input_tokens"`
	OutputTokens  uint64   `json:"outputTokens" orm:"output_tokens"`
	EstimatedCost *float64 `json:"estimatedCost" orm:"estimated_cost"`
}

type Breakdown struct {
	Name          string   `json:"name" orm:"name"`
	Requests      int64    `json:"requests" orm:"requests"`
	TotalTokens   uint64   `json:"totalTokens" orm:"total_tokens"`
	EstimatedCost *float64 `json:"estimatedCost" orm:"estimated_cost"`
}

type Dashboard struct {
	Summary   Summary      `json:"summary"`
	Trend     []TrendPoint `json:"trend"`
	ByModel   []Breakdown  `json:"byModel"`
	ByChannel []Breakdown  `json:"byChannel"`
}

type LogView struct {
	Id                uint64    `json:"id" orm:"id"`
	RequestId         string    `json:"requestId" orm:"request_id"`
	APIKeyName        string    `json:"apiKeyName" orm:"api_key_name"`
	ChannelName       string    `json:"channelName" orm:"channel_name"`
	Endpoint          string    `json:"endpoint" orm:"endpoint"`
	RequestedModel    string    `json:"requestedModel" orm:"requested_model"`
	UpstreamModel     string    `json:"upstreamModel" orm:"upstream_model"`
	HttpStatus        uint      `json:"httpStatus" orm:"http_status"`
	IsStream          int       `json:"isStream" orm:"is_stream"`
	InputTokens       *uint64   `json:"inputTokens" orm:"input_tokens"`
	CachedInputTokens *uint64   `json:"cachedInputTokens" orm:"cached_input_tokens"`
	OutputTokens      *uint64   `json:"outputTokens" orm:"output_tokens"`
	TotalTokens       *uint64   `json:"totalTokens" orm:"total_tokens"`
	EstimatedCost     *float64  `json:"estimatedCost" orm:"estimated_cost"`
	DurationMs        uint64    `json:"durationMs" orm:"duration_ms"`
	FirstTokenMs      *uint64   `json:"firstTokenMs" orm:"first_token_ms"`
	Attempts          uint      `json:"attempts" orm:"attempts"`
	ErrorMessage      string    `json:"errorMessage" orm:"error_message"`
	CreatedAt         time.Time `json:"createdAt" orm:"created_at"`
}

type LogPage struct {
	Items    []LogView `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

func New() *Service {
	return &Service{}
}

func (s *Service) Record(ctx context.Context, input RecordInput) error {
	data := do.UsageLogs{
		RequestId:      input.RequestID,
		UserId:         input.UserID,
		ApiKeyId:       input.APIKeyID,
		ChannelId:      input.ChannelID,
		Endpoint:       input.Endpoint,
		RequestedModel: input.RequestedModel,
		UpstreamModel:  input.UpstreamModel,
		HttpStatus:     input.HTTPStatus,
		IsStream:       boolInt(input.Stream),
		DurationMs:     input.DurationMs,
		Attempts:       input.Attempts,
		ErrorMessage:   truncate(input.ErrorMessage, 1024),
	}
	if input.Tokens.Input != nil {
		data.InputTokens = *input.Tokens.Input
	}
	if input.Tokens.CachedInput != nil {
		data.CachedInputTokens = *input.Tokens.CachedInput
	}
	if input.Tokens.Output != nil {
		data.OutputTokens = *input.Tokens.Output
	}
	if input.Tokens.Total != nil {
		data.TotalTokens = *input.Tokens.Total
	}
	if input.EstimatedCost != nil {
		data.EstimatedCost = *input.EstimatedCost
	}
	if input.FirstTokenMs != nil {
		data.FirstTokenMs = *input.FirstTokenMs
	}
	_, err := dao.UsageLogs.Ctx(ctx).Data(data).Insert()
	return gerror.Wrap(err, "record usage")
}

func (s *Service) Dashboard(ctx context.Context, days int) (Dashboard, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	start := time.Now().AddDate(0, 0, -days+1).Truncate(24 * time.Hour)
	var result Dashboard
	base := dao.UsageLogs.Ctx(ctx).WhereGTE(dao.UsageLogs.Columns().CreatedAt, start)
	if err := base.Clone().Fields(`
		COUNT(*) AS requests,
		COALESCE(SUM(CASE WHEN http_status BETWEEN 200 AND 299 THEN 1 ELSE 0 END),0) AS successes,
		COALESCE(SUM(input_tokens),0) AS input_tokens,
		COALESCE(SUM(output_tokens),0) AS output_tokens,
		COALESCE(SUM(total_tokens),0) AS total_tokens,
		SUM(estimated_cost) AS estimated_cost,
		COALESCE(AVG(duration_ms),0) AS average_latency`).Scan(&result.Summary); err != nil {
		return result, gerror.Wrap(err, "load dashboard summary")
	}
	if err := base.Clone().Fields(`
		DATE_FORMAT(created_at,'%Y-%m-%d') AS bucket,
		COUNT(*) AS requests,
		COALESCE(SUM(input_tokens),0) AS input_tokens,
		COALESCE(SUM(output_tokens),0) AS output_tokens,
		SUM(estimated_cost) AS estimated_cost`).
		Group("bucket").OrderAsc("bucket").Scan(&result.Trend); err != nil {
		return result, gerror.Wrap(err, "load usage trend")
	}
	if err := base.Clone().Fields(`requested_model AS name,COUNT(*) AS requests,COALESCE(SUM(total_tokens),0) AS total_tokens,SUM(estimated_cost) AS estimated_cost`).
		Group(dao.UsageLogs.Columns().RequestedModel).OrderDesc("requests").Limit(8).Scan(&result.ByModel); err != nil {
		return result, gerror.Wrap(err, "load model breakdown")
	}
	if err := base.Clone().As("u").Fields(`COALESCE(c.name,'不可用渠道') AS name,COUNT(*) AS requests,COALESCE(SUM(u.total_tokens),0) AS total_tokens,SUM(u.estimated_cost) AS estimated_cost`).
		LeftJoin(dao.Channels.Table()+" c", "c.id=u.channel_id").Group("u.channel_id,c.name").OrderDesc("requests").Limit(8).Scan(&result.ByChannel); err != nil {
		return result, gerror.Wrap(err, "load channel breakdown")
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, page, pageSize int, modelName string, channelID, apiKeyID uint64) (LogPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := dao.UsageLogs.Ctx(ctx).As("u")
	if modelName != "" {
		query = query.WhereLike("u.requested_model", "%"+modelName+"%")
	}
	if channelID > 0 {
		query = query.Where("u.channel_id", channelID)
	}
	if apiKeyID > 0 {
		query = query.Where("u.api_key_id", apiKeyID)
	}
	total, err := query.Clone().Count()
	if err != nil {
		return LogPage{}, gerror.Wrap(err, "count usage logs")
	}
	var items []LogView
	err = query.Fields("u.*,k.name AS api_key_name,c.name AS channel_name").
		LeftJoin(dao.ApiKeys.Table()+" k", "k.id=u.api_key_id").
		LeftJoin(dao.Channels.Table()+" c", "c.id=u.channel_id").
		OrderDesc("u.id").Page(page, pageSize).Scan(&items)
	return LogPage{Items: items, Total: total, Page: page, PageSize: pageSize}, gerror.Wrap(err, "list usage logs")
}

func EstimateCost(tokens TokenUsage, rates PriceRates) *decimal.Decimal {
	cost := decimal.Zero
	priced := false
	inputRemaining := remainingTokens(tokens.Input)
	outputRemaining := remainingTokens(tokens.Output)
	denominator := decimal.NewFromInt(1_000_000)

	chargeInputComponent := func(tokens *uint64, preferred, fallback *float64) {
		amount := tokenCount(tokens)
		if inputRemaining != nil && amount > *inputRemaining {
			amount = *inputRemaining
		}
		if inputRemaining != nil {
			*inputRemaining -= amount
		}
		rate := preferred
		if rate == nil {
			rate = fallback
		}
		if rate == nil || amount == 0 {
			return
		}
		cost = cost.Add(decimal.NewFromInt(int64(amount)).Mul(decimal.NewFromFloat(*rate)).Div(denominator))
		priced = true
	}
	chargeOutputComponent := func(tokens *uint64, preferred, fallback *float64) {
		amount := tokenCount(tokens)
		if outputRemaining != nil && amount > *outputRemaining {
			amount = *outputRemaining
		}
		if outputRemaining != nil {
			*outputRemaining -= amount
		}
		rate := preferred
		if rate == nil {
			rate = fallback
		}
		if rate == nil || amount == 0 {
			return
		}
		cost = cost.Add(decimal.NewFromInt(int64(amount)).Mul(decimal.NewFromFloat(*rate)).Div(denominator))
		priced = true
	}

	chargeInputComponent(tokens.CachedInput, rates.CachedInput, rates.Input)
	chargeInputComponent(tokens.CacheWrite, rates.CacheWrite, rates.Input)
	chargeInputComponent(tokens.ImageInput, rates.ImageInput, rates.Input)
	chargeInputComponent(tokens.AudioInput, rates.AudioInput, rates.Input)
	if inputRemaining != nil && rates.Input != nil && *inputRemaining > 0 {
		cost = cost.Add(decimal.NewFromInt(int64(*inputRemaining)).Mul(decimal.NewFromFloat(*rates.Input)).Div(denominator))
		priced = true
	}
	chargeOutputComponent(tokens.AudioOutput, rates.AudioOutput, rates.Output)
	if outputRemaining != nil && rates.Output != nil && *outputRemaining > 0 {
		cost = cost.Add(decimal.NewFromInt(int64(*outputRemaining)).Mul(decimal.NewFromFloat(*rates.Output)).Div(denominator))
		priced = true
	}
	if rates.Request != nil {
		cost = cost.Add(decimal.NewFromFloat(*rates.Request))
		priced = true
	}
	if !priced {
		return nil
	}
	return &cost
}

func tokenCount(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func remainingTokens(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
