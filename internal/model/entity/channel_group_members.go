// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelGroupMembers is the golang structure for table channel_group_members.
type ChannelGroupMembers struct {
	ChannelGroupId uint64    `json:"channelGroupId" orm:"channel_group_id" ` //
	ChannelId      uint64    `json:"channelId"      orm:"channel_id"       ` //
	CreatedAt      time.Time `json:"createdAt"      orm:"created_at"       ` //
}
