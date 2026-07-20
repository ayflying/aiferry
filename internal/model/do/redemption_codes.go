// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// RedemptionCodes is the golang structure of table redemption_codes for DAO operations like Where/Data.
type RedemptionCodes struct {
	g.Meta           `orm:"table:redemption_codes, do:true"`
	Id               any //
	Name             any //
	Code             any //
	Amount           any //
	ExpiresAt        any //
	RedeemedByUserId any //
	RedeemedAt       any //
	CreatedByUserId  any //
	CreatedAt        any //
	UpdatedAt        any //
}
