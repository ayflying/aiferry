// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelModels is the golang structure for table channel_models.
type ChannelModels struct {
	Id                uint64    `json:"id"                orm:"id"                   description:""` //
	ChannelId         uint64    `json:"channelId"         orm:"channel_id"           description:""` //
	PublicName        string    `json:"publicName"        orm:"public_name"          description:""` //
	UpstreamName      string    `json:"upstreamName"      orm:"upstream_name"        description:""` //
	Discovered        int       `json:"discovered"        orm:"discovered"           description:""` //
	Enabled           int       `json:"enabled"           orm:"enabled"              description:""` //
	InputPrice        float64   `json:"inputPrice"        orm:"input_price"          description:""` //
	CachedInputPrice  float64   `json:"cachedInputPrice"  orm:"cached_input_price"   description:""` //
	OutputPrice       float64   `json:"outputPrice"       orm:"output_price"         description:""` //
	LastTestEndpoint  string    `json:"lastTestEndpoint"  orm:"last_test_endpoint"   description:""` //
	LastTestStatus    string    `json:"lastTestStatus"    orm:"last_test_status"     description:""` //
	LastTestLatencyMs uint      `json:"lastTestLatencyMs" orm:"last_test_latency_ms" description:""` //
	LastTestError     string    `json:"lastTestError"     orm:"last_test_error"      description:""` //
	LastTestAt        time.Time `json:"lastTestAt"        orm:"last_test_at"         description:""` //
	CreatedAt         time.Time `json:"createdAt"         orm:"created_at"           description:""` //
	UpdatedAt         time.Time `json:"updatedAt"         orm:"updated_at"           description:""` //
	DeletedAt         time.Time `json:"deletedAt"         orm:"deleted_at"           description:""` //
}
