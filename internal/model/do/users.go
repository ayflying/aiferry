// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// Users is the golang structure of table users for DAO operations like Where/Data.
type Users struct {
	g.Meta           `orm:"table:users, do:true"`
	Id               any //
	Name             any //
	Email            any //
	Role             any //
	Status           any //
	Balance          any //
	IdentityProvider any //
	IdentitySubject  any //
	AvatarUrl        any //
	GroupsJson       any //
	LastLoginAt      any //
	CreatedAt        any //
	UpdatedAt        any //
	DeletedAt        any //
}
