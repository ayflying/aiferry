// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// PriceSources is the golang structure for table price_sources.
type PriceSources struct {
	Id         uint64    `json:"id"         orm:"id"          description:""` //
	Name       string    `json:"name"       orm:"name"        description:""` //
	Code       string    `json:"code"       orm:"code"        description:""` //
	ConfigJson string    `json:"configJson" orm:"config_json" description:""` //
	Status     int       `json:"status"     orm:"status"      description:""` //
	BuiltIn    int       `json:"builtIn"    orm:"built_in"    description:""` //
	CreatedAt  time.Time `json:"createdAt"  orm:"created_at"  description:""` //
	UpdatedAt  time.Time `json:"updatedAt"  orm:"updated_at"  description:""` //
	DeletedAt  time.Time `json:"deletedAt"  orm:"deleted_at"  description:""` //
}
