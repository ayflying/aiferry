// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ModelPriceRules is the golang structure for table model_price_rules.
type ModelPriceRules struct {
	Id             uint64    `json:"id"             orm:"id"               description:""` //
	ChannelModelId uint64    `json:"channelModelId" orm:"channel_model_id" description:""` //
	ModelName      string    `json:"modelName"      orm:"model_name"       description:""` //
	Name           string    `json:"name"           orm:"name"             description:""` //
	Source         string    `json:"source"         orm:"source"           description:""` //
	SourceRef      string    `json:"sourceRef"      orm:"source_ref"       description:""` //
	Priority       int       `json:"priority"       orm:"priority"         description:""` //
	Currency       string    `json:"currency"       orm:"currency"         description:""` //
	ConditionsJson string    `json:"conditionsJson" orm:"conditions_json"  description:""` //
	RatesJson      string    `json:"ratesJson"      orm:"rates_json"       description:""` //
	Status         int       `json:"status"         orm:"status"           description:""` //
	SyncedAt       time.Time `json:"syncedAt"       orm:"synced_at"        description:""` //
	CreatedAt      time.Time `json:"createdAt"      orm:"created_at"       description:""` //
	UpdatedAt      time.Time `json:"updatedAt"      orm:"updated_at"       description:""` //
	DeletedAt      time.Time `json:"deletedAt"      orm:"deleted_at"       description:""` //
}
