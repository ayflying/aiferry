// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelGroups is the golang structure for table channel_groups.
type ChannelGroups struct {
	Id          uint64    `json:"id"          orm:"id"          ` //
	Name        string    `json:"name"        orm:"name"        ` //
	Code        string    `json:"code"        orm:"code"        ` //
	Description string    `json:"description" orm:"description" ` //
	Status      int       `json:"status"      orm:"status"      ` //
	CreatedAt   time.Time `json:"createdAt"   orm:"created_at"  ` //
	UpdatedAt   time.Time `json:"updatedAt"   orm:"updated_at"  ` //
	DeletedAt   time.Time `json:"deletedAt"   orm:"deleted_at"  ` //
}
