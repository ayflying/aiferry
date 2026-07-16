// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// UsageLogs is the golang structure for table usage_logs.
type UsageLogs struct {
	Id                uint64    `json:"id"                orm:"id"                  ` //
	RequestId         string    `json:"requestId"         orm:"request_id"          ` //
	UserId            uint64    `json:"userId"            orm:"user_id"             ` //
	ApiKeyId          uint64    `json:"apiKeyId"          orm:"api_key_id"          ` //
	ChannelId         uint64    `json:"channelId"         orm:"channel_id"          ` //
	Endpoint          string    `json:"endpoint"          orm:"endpoint"            ` //
	RequestedModel    string    `json:"requestedModel"    orm:"requested_model"     ` //
	UpstreamModel     string    `json:"upstreamModel"     orm:"upstream_model"      ` //
	HttpStatus        uint      `json:"httpStatus"        orm:"http_status"         ` //
	IsStream          int       `json:"isStream"          orm:"is_stream"           ` //
	InputTokens       uint64    `json:"inputTokens"       orm:"input_tokens"        ` //
	CachedInputTokens uint64    `json:"cachedInputTokens" orm:"cached_input_tokens" ` //
	OutputTokens      uint64    `json:"outputTokens"      orm:"output_tokens"       ` //
	TotalTokens       uint64    `json:"totalTokens"       orm:"total_tokens"        ` //
	EstimatedCost     float64   `json:"estimatedCost"     orm:"estimated_cost"      ` //
	DurationMs        uint64    `json:"durationMs"        orm:"duration_ms"         ` //
	FirstTokenMs      uint64    `json:"firstTokenMs"      orm:"first_token_ms"      ` //
	Attempts          uint      `json:"attempts"          orm:"attempts"            ` //
	ErrorMessage      string    `json:"errorMessage"      orm:"error_message"       ` //
	CreatedAt         time.Time `json:"createdAt"         orm:"created_at"          ` //
}
