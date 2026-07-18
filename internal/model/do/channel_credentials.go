// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelCredentials is the golang structure of table channel_credentials for DAO operations like Where/Data.
type ChannelCredentials struct {
	g.Meta                 `orm:"table:channel_credentials, do:true"`
	Id                     any //
	ChannelId              any //
	KeyPrefix              any //
	KeyHash                any //
	ApiKeyCipher           any //
	Status                 any //
	AutoDisabledAt         any //
	AutoDisabledReason     any //
	AutoDisabledStatusCode any //
	AutoDisabledSource     any //
	LastCostUsed           any //
	LastCostRemaining      any //
	LastCostCurrency       any //
	LastCostAt             any //
	CreatedAt              any //
	UpdatedAt              any //
	DeletedAt              any //
}
