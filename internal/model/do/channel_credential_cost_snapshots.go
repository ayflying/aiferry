// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelCredentialCostSnapshots is the golang structure of table channel_credential_cost_snapshots for DAO operations like Where/Data.
type ChannelCredentialCostSnapshots struct {
	g.Meta              `orm:"table:channel_credential_cost_snapshots, do:true"`
	Id                  any //
	ChannelCredentialId any //
	Mode                any //
	UsedAmount          any //
	RemainingAmount     any //
	Currency            any //
	PeriodStart         any //
	PeriodEnd           any //
	QueriedAt           any //
	CreatedAt           any //
}
