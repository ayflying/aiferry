// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelCredentials is the golang structure for table channel_credentials.
type ChannelCredentials struct {
	Id                     uint64    `json:"id"                     orm:"id"                        description:""` //
	ChannelId              uint64    `json:"channelId"              orm:"channel_id"                description:""` //
	KeyPrefix              string    `json:"keyPrefix"              orm:"key_prefix"                description:""` //
	KeyHash                string    `json:"keyHash"                orm:"key_hash"                  description:""` //
	ApiKeyCipher           string    `json:"apiKeyCipher"           orm:"api_key_cipher"            description:""` //
	Status                 int       `json:"status"                 orm:"status"                    description:""` //
	AutoDisabledAt         time.Time `json:"autoDisabledAt"         orm:"auto_disabled_at"          description:""` //
	AutoDisabledReason     string    `json:"autoDisabledReason"     orm:"auto_disabled_reason"      description:""` //
	AutoDisabledStatusCode uint      `json:"autoDisabledStatusCode" orm:"auto_disabled_status_code" description:""` //
	AutoDisabledSource     string    `json:"autoDisabledSource"     orm:"auto_disabled_source"      description:""` //
	LastCostUsed           float64   `json:"lastCostUsed"           orm:"last_cost_used"            description:""` //
	LastCostRemaining      float64   `json:"lastCostRemaining"      orm:"last_cost_remaining"       description:""` //
	LastCostCurrency       string    `json:"lastCostCurrency"       orm:"last_cost_currency"        description:""` //
	LastCostAt             time.Time `json:"lastCostAt"             orm:"last_cost_at"              description:""` //
	CreatedAt              time.Time `json:"createdAt"              orm:"created_at"                description:""` //
	UpdatedAt              time.Time `json:"updatedAt"              orm:"updated_at"                description:""` //
	DeletedAt              time.Time `json:"deletedAt"              orm:"deleted_at"                description:""` //
}
