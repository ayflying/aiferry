// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ModelPriceRules is the golang structure of table model_price_rules for DAO operations like Where/Data.
type ModelPriceRules struct {
	g.Meta         `orm:"table:model_price_rules, do:true"`
	Id             any //
	ChannelModelId any //
	ModelName      any //
	Name           any //
	Source         any //
	SourceRef      any //
	Priority       any //
	Currency       any //
	ConditionsJson any //
	RatesJson      any //
	Status         any //
	SyncedAt       any //
	CreatedAt      any //
	UpdatedAt      any //
	DeletedAt      any //
}
