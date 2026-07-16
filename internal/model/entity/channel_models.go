// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelModels is the golang structure for table channel_models.
type ChannelModels struct {
	Id                uint64    `json:"id"                orm:"id"                   ` //
	ChannelId         uint64    `json:"channelId"         orm:"channel_id"           ` //
	PublicName        string    `json:"publicName"        orm:"public_name"          ` //
	UpstreamName      string    `json:"upstreamName"      orm:"upstream_name"        ` //
	Discovered        int       `json:"discovered"        orm:"discovered"           ` //
	Enabled           int       `json:"enabled"           orm:"enabled"              ` //
	InputPrice        float64   `json:"inputPrice"        orm:"input_price"          ` //
	CachedInputPrice  float64   `json:"cachedInputPrice"  orm:"cached_input_price"   ` //
	OutputPrice       float64   `json:"outputPrice"       orm:"output_price"         ` //
	LastTestEndpoint  string    `json:"lastTestEndpoint"  orm:"last_test_endpoint"   ` //
	LastTestStatus    string    `json:"lastTestStatus"    orm:"last_test_status"     ` //
	LastTestLatencyMs uint      `json:"lastTestLatencyMs" orm:"last_test_latency_ms" ` //
	LastTestError     string    `json:"lastTestError"     orm:"last_test_error"      ` //
	LastTestAt        time.Time `json:"lastTestAt"        orm:"last_test_at"         ` //
	CreatedAt         time.Time `json:"createdAt"         orm:"created_at"           ` //
	UpdatedAt         time.Time `json:"updatedAt"         orm:"updated_at"           ` //
	DeletedAt         time.Time `json:"deletedAt"         orm:"deleted_at"           ` //
}
