// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ApiKeys is the golang structure of table api_keys for DAO operations like Where/Data.
type ApiKeys struct {
	g.Meta      `orm:"table:api_keys, do:true"`
	Id          any //
	UserId      any //
	Name        any //
	KeyPrefix   any //
	KeyHash     any //
	Status      any //
	SpendLimit  any //
	SpentAmount any //
	ExpiresAt   any //
	LastUsedAt  any //
	CreatedAt   any //
	UpdatedAt   any //
	DeletedAt   any //
}
