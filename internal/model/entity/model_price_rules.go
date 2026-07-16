// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"github.com/gogf/gf/v2/os/gtime"
)

// ModelPriceRules is the golang structure for table model_price_rules.
type ModelPriceRules struct {
	Id             uint64      `json:"id"             orm:"id"               ` //
	ChannelModelId uint64      `json:"channelModelId" orm:"channel_model_id" ` //
	ModelName      string      `json:"modelName"      orm:"model_name"       ` //
	Name           string      `json:"name"           orm:"name"             ` //
	Source         string      `json:"source"         orm:"source"           ` //
	SourceRef      string      `json:"sourceRef"      orm:"source_ref"       ` //
	Priority       int         `json:"priority"       orm:"priority"         ` //
	Currency       string      `json:"currency"       orm:"currency"         ` //
	ConditionsJson string      `json:"conditionsJson" orm:"conditions_json"  ` //
	RatesJson      string      `json:"ratesJson"      orm:"rates_json"       ` //
	Status         int         `json:"status"         orm:"status"           ` //
	SyncedAt       *gtime.Time `json:"syncedAt"       orm:"synced_at"        ` //
	CreatedAt      *gtime.Time `json:"createdAt"      orm:"created_at"       ` //
	UpdatedAt      *gtime.Time `json:"updatedAt"      orm:"updated_at"       ` //
	DeletedAt      *gtime.Time `json:"deletedAt"      orm:"deleted_at"       ` //
}
