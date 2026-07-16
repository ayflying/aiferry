// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelCostSnapshots is the golang structure of table channel_cost_snapshots for DAO operations like Where/Data.
type ChannelCostSnapshots struct {
	g.Meta          `orm:"table:channel_cost_snapshots, do:true"`
	Id              any //
	ChannelId       any //
	Mode            any //
	UsedAmount      any //
	RemainingAmount any //
	Currency        any //
	PeriodStart     any //
	PeriodEnd       any //
	QueriedAt       any //
	CreatedAt       any //
}
