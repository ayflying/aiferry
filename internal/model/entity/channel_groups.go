// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelGroups is the golang structure for table channel_groups.
type ChannelGroups struct {
	Id          uint64    `json:"id"          orm:"id"          description:""` //
	Name        string    `json:"name"        orm:"name"        description:""` //
	Code        string    `json:"code"        orm:"code"        description:""` //
	Description string    `json:"description" orm:"description" description:""` //
	Status      int       `json:"status"      orm:"status"      description:""` //
	CreatedAt   time.Time `json:"createdAt"   orm:"created_at"  description:""` //
	UpdatedAt   time.Time `json:"updatedAt"   orm:"updated_at"  description:""` //
	DeletedAt   time.Time `json:"deletedAt"   orm:"deleted_at"  description:""` //
}
