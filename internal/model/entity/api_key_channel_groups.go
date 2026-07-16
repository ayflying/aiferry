// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKeyChannelGroups is the golang structure for table api_key_channel_groups.
type ApiKeyChannelGroups struct {
	ApiKeyId       uint64    `json:"apiKeyId"       orm:"api_key_id"       ` //
	ChannelGroupId uint64    `json:"channelGroupId" orm:"channel_group_id" ` //
	CreatedAt      time.Time `json:"createdAt"      orm:"created_at"       ` //
}
