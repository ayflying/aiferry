// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ModelPrices is the golang structure of table model_prices for DAO operations like Where/Data.
type ModelPrices struct {
	g.Meta           `orm:"table:model_prices, do:true"`
	PublicName       any //
	BillingMode      any //
	InputPrice       any //
	CachedInputPrice any //
	CacheWritePrice  any //
	OutputPrice      any //
	ImageInputPrice  any //
	AudioInputPrice  any //
	AudioOutputPrice any //
	RequestPrice     any //
	CreatedAt        any //
	UpdatedAt        any //
	DeletedAt        any //
}
