// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ModelPriceRules is the golang structure for table model_price_rules.
type ModelPriceRules struct {
	Id             uint64    `json:"id"             orm:"id"               ` //
	ChannelModelId uint64    `json:"channelModelId" orm:"channel_model_id" ` //
	Name           string    `json:"name"           orm:"name"             ` //
	Source         string    `json:"source"         orm:"source"           ` //
	SourceRef      string    `json:"sourceRef"      orm:"source_ref"       ` //
	Priority       int       `json:"priority"       orm:"priority"         ` //
	Currency       string    `json:"currency"       orm:"currency"         ` //
	ConditionsJson string    `json:"conditionsJson" orm:"conditions_json"  ` //
	RatesJson      string    `json:"ratesJson"      orm:"rates_json"       ` //
	Status         int       `json:"status"         orm:"status"           ` //
	SyncedAt       time.Time `json:"syncedAt"       orm:"synced_at"        ` //
	CreatedAt      time.Time `json:"createdAt"      orm:"created_at"       ` //
	UpdatedAt      time.Time `json:"updatedAt"      orm:"updated_at"       ` //
	DeletedAt      time.Time `json:"deletedAt"      orm:"deleted_at"       ` //
}
