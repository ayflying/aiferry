// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKeys is the golang structure for table api_keys.
type ApiKeys struct {
	Id         uint64    `json:"id"         orm:"id"           description:""` //
	UserId     uint64    `json:"userId"     orm:"user_id"      description:""` //
	Name       string    `json:"name"       orm:"name"         description:""` //
	KeyPrefix  string    `json:"keyPrefix"  orm:"key_prefix"   description:""` //
	KeyHash    string    `json:"keyHash"    orm:"key_hash"     description:""` //
	Status     int       `json:"status"     orm:"status"       description:""` //
	ExpiresAt  time.Time `json:"expiresAt"  orm:"expires_at"   description:""` //
	LastUsedAt time.Time `json:"lastUsedAt" orm:"last_used_at" description:""` //
	CreatedAt  time.Time `json:"createdAt"  orm:"created_at"   description:""` //
	UpdatedAt  time.Time `json:"updatedAt"  orm:"updated_at"   description:""` //
	DeletedAt  time.Time `json:"deletedAt"  orm:"deleted_at"   description:""` //
}
