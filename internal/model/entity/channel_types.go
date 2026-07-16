// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelTypes is the golang structure for table channel_types.
type ChannelTypes struct {
	Id         uint64    `json:"id"         orm:"id"          ` //
	Name       string    `json:"name"       orm:"name"        ` //
	Code       string    `json:"code"       orm:"code"        ` //
	ConfigJson string    `json:"configJson" orm:"config_json" ` //
	Status     int       `json:"status"     orm:"status"      ` //
	BuiltIn    int       `json:"builtIn"    orm:"built_in"    ` //
	CreatedAt  time.Time `json:"createdAt"  orm:"created_at"  ` //
	UpdatedAt  time.Time `json:"updatedAt"  orm:"updated_at"  ` //
	DeletedAt  time.Time `json:"deletedAt"  orm:"deleted_at"  ` //
}
