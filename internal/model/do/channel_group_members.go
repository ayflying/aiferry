// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelGroupMembers is the golang structure of table channel_group_members for DAO operations like Where/Data.
type ChannelGroupMembers struct {
	g.Meta         `orm:"table:channel_group_members, do:true"`
	ChannelGroupId any //
	ChannelId      any //
	CreatedAt      any //
}
