package usage

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

type Service struct {
	location IPLocator
}

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
	RequestID           string
	UserID              uint64
	APIKeyID            uint64
	ChannelID           uint64
	ChannelCredentialID uint64
	Endpoint            string
	UpstreamEndpoint    string
	ProtocolConversion  string
	ClientIP            string
	IPLocation          string
	RequestedModel      string
	UpstreamModel       string
	HTTPStatus          int
	Stream              bool
	Tokens              TokenUsage
	EstimatedCost       *decimal.Decimal
	BillingDetails      *BillingBreakdown
	DurationMs          int64
	FirstTokenMs        *int64
	Attempts            int
	ErrorMessage        string
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

type HourlyCostPoint struct {
	Bucket        string  `json:"bucket" orm:"bucket"`
	EstimatedCost float64 `json:"estimatedCost" orm:"estimated_cost"`
}

type RecentCostModel struct {
	Name   string            `json:"name"`
	Points []HourlyCostPoint `json:"points"`
}

type RecentCostDistribution struct {
	TotalEstimatedCost float64           `json:"totalEstimatedCost"`
	Models             []RecentCostModel `json:"models"`
}

type Dashboard struct {
	Summary    Summary                `json:"summary"`
	Trend      []TrendPoint           `json:"trend"`
	ByModel    []Breakdown            `json:"byModel"`
	ByChannel  []Breakdown            `json:"byChannel"`
	RecentCost RecentCostDistribution `json:"recentCost"`
}

type UserSummary struct {
	Days          int     `json:"days"`
	Requests      int64   `json:"requests" orm:"requests"`
	Successes     int64   `json:"successes" orm:"successes"`
	InputTokens   uint64  `json:"inputTokens" orm:"input_tokens"`
	OutputTokens  uint64  `json:"outputTokens" orm:"output_tokens"`
	TotalTokens   uint64  `json:"totalTokens" orm:"total_tokens"`
	EstimatedCost float64 `json:"estimatedCost" orm:"estimated_cost"`
}

type LogView struct {
	Id                 uint64            `json:"id" orm:"id"`
	RequestId          string            `json:"requestId" orm:"request_id"`
	UserId             uint64            `json:"userId" orm:"user_id"`
	UserName           string            `json:"userName" orm:"user_name"`
	APIKeyName         string            `json:"apiKeyName" orm:"api_key_name"`
	ChannelName        string            `json:"channelName" orm:"channel_name"`
	Endpoint           string            `json:"endpoint" orm:"endpoint"`
	UpstreamEndpoint   string            `json:"upstreamEndpoint" orm:"upstream_endpoint"`
	ProtocolConversion string            `json:"protocolConversion" orm:"protocol_conversion"`
	ClientIP           string            `json:"clientIp" orm:"client_ip"`
	IPLocation         string            `json:"ipLocation" orm:"ip_location"`
	RequestedModel     string            `json:"requestedModel" orm:"requested_model"`
	UpstreamModel      string            `json:"upstreamModel" orm:"upstream_model"`
	HttpStatus         uint              `json:"httpStatus" orm:"http_status"`
	IsStream           int               `json:"isStream" orm:"is_stream"`
	InputTokens        *uint64           `json:"inputTokens" orm:"input_tokens"`
	CachedInputTokens  *uint64           `json:"cachedInputTokens" orm:"cached_input_tokens"`
	OutputTokens       *uint64           `json:"outputTokens" orm:"output_tokens"`
	TotalTokens        *uint64           `json:"totalTokens" orm:"total_tokens"`
	EstimatedCost      *float64          `json:"estimatedCost" orm:"estimated_cost"`
	BillingDetailsJSON string            `json:"-" orm:"billing_details_json"`
	BillingDetails     *BillingBreakdown `json:"billingDetails,omitempty" orm:"-"`
	DurationMs         uint64            `json:"durationMs" orm:"duration_ms"`
	FirstTokenMs       *uint64           `json:"firstTokenMs" orm:"first_token_ms"`
	Attempts           uint              `json:"attempts" orm:"attempts"`
	ErrorMessage       string            `json:"errorMessage" orm:"error_message"`
	CreatedAt          time.Time         `json:"createdAt" orm:"created_at"`
}

type LogSummary struct {
	Requests      int64   `json:"requests" orm:"requests"`
	EstimatedCost float64 `json:"estimatedCost" orm:"estimated_cost"`
}

type LogPage struct {
	Items    []LogView  `json:"items"`
	Summary  LogSummary `json:"summary"`
	StartAt  time.Time  `json:"startAt"`
	EndAt    time.Time  `json:"endAt"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

func New(location IPLocator) *Service {
	return &Service{location: location}
}

func (s *Service) Record(ctx context.Context, input RecordInput) error {
	input.IPLocation = s.resolveIPLocation(input.ClientIP, input.IPLocation)
	data := do.UsageLogs{
		RequestId:          input.RequestID,
		UserId:             input.UserID,
		ApiKeyId:           input.APIKeyID,
		ChannelId:          input.ChannelID,
		Endpoint:           input.Endpoint,
		UpstreamEndpoint:   input.UpstreamEndpoint,
		ProtocolConversion: input.ProtocolConversion,
		ClientIp:           input.ClientIP,
		IpLocation:         input.IPLocation,
		RequestedModel:     input.RequestedModel,
		UpstreamModel:      input.UpstreamModel,
		HttpStatus:         input.HTTPStatus,
		IsStream:           boolInt(input.Stream),
		DurationMs:         input.DurationMs,
		Attempts:           input.Attempts,
		ErrorMessage:       truncate(input.ErrorMessage, 1024),
	}
	if input.ChannelCredentialID > 0 {
		data.ChannelCredentialId = input.ChannelCredentialID
	} else {
		data.ChannelCredentialId = gdb.Raw("NULL")
	}
	if input.APIKeyID == 0 {
		data.ApiKeyId = gdb.Raw("NULL")
	}
	if input.ClientIP == "" {
		data.ClientIp = gdb.Raw("NULL")
	}
	if input.IPLocation == "" {
		data.IpLocation = gdb.Raw("NULL")
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
	if input.BillingDetails != nil {
		encoded, err := input.BillingDetails.JSON()
		if err != nil {
			return gerror.Wrap(err, "encode billing details")
		}
		data.BillingDetailsJson = encoded
	}
	if input.FirstTokenMs != nil {
		data.FirstTokenMs = *input.FirstTokenMs
	}
	_, err := dao.UsageLogs.Ctx(ctx).Data(data).Insert()
	return gerror.Wrap(err, "record usage")
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
