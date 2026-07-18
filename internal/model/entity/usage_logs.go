// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// UsageLogs is the golang structure for table usage_logs.
type UsageLogs struct {
	Id                  uint64    `json:"id"                  orm:"id"                    description:""` //
	RequestId           string    `json:"requestId"           orm:"request_id"            description:""` //
	UserId              uint64    `json:"userId"              orm:"user_id"               description:""` //
	ApiKeyId            uint64    `json:"apiKeyId"            orm:"api_key_id"            description:""` //
	ChannelId           uint64    `json:"channelId"           orm:"channel_id"            description:""` //
	ChannelCredentialId uint64    `json:"channelCredentialId" orm:"channel_credential_id" description:""` //
	Endpoint            string    `json:"endpoint"            orm:"endpoint"              description:""` //
	RequestedModel      string    `json:"requestedModel"      orm:"requested_model"       description:""` //
	UpstreamModel       string    `json:"upstreamModel"       orm:"upstream_model"        description:""` //
	HttpStatus          uint      `json:"httpStatus"          orm:"http_status"           description:""` //
	IsStream            int       `json:"isStream"            orm:"is_stream"             description:""` //
	InputTokens         uint64    `json:"inputTokens"         orm:"input_tokens"          description:""` //
	CachedInputTokens   uint64    `json:"cachedInputTokens"   orm:"cached_input_tokens"   description:""` //
	OutputTokens        uint64    `json:"outputTokens"        orm:"output_tokens"         description:""` //
	TotalTokens         uint64    `json:"totalTokens"         orm:"total_tokens"          description:""` //
	EstimatedCost       float64   `json:"estimatedCost"       orm:"estimated_cost"        description:""` //
	DurationMs          uint64    `json:"durationMs"          orm:"duration_ms"           description:""` //
	FirstTokenMs        uint64    `json:"firstTokenMs"        orm:"first_token_ms"        description:""` //
	Attempts            uint      `json:"attempts"            orm:"attempts"              description:""` //
	ErrorMessage        string    `json:"errorMessage"        orm:"error_message"         description:""` //
	CreatedAt           time.Time `json:"createdAt"           orm:"created_at"            description:""` //
}
