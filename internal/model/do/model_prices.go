// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

// ModelPrices is the golang structure of table model_prices for DAO operations like Where/Data.
type ModelPrices struct {
	g.Meta           `orm:"table:model_prices, do:true"`
	PublicName       any         //
	InputPrice       any         //
	CachedInputPrice any         //
	OutputPrice      any         //
	CreatedAt        *gtime.Time //
	UpdatedAt        *gtime.Time //
	DeletedAt        *gtime.Time //
}
