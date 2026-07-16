// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKeys is the golang structure for table api_keys.
type ApiKeys struct {
	Id          uint64    `json:"id"          orm:"id"           ` //
	UserId      uint64    `json:"userId"      orm:"user_id"      ` //
	Name        string    `json:"name"        orm:"name"         ` //
	KeyPrefix   string    `json:"keyPrefix"   orm:"key_prefix"   ` //
	KeyHash     string    `json:"keyHash"     orm:"key_hash"     ` //
	Status      int       `json:"status"      orm:"status"       ` //
	SpendLimit  float64   `json:"spendLimit"  orm:"spend_limit"  ` //
	SpentAmount float64   `json:"spentAmount" orm:"spent_amount" ` //
	ExpiresAt   time.Time `json:"expiresAt"   orm:"expires_at"   ` //
	LastUsedAt  time.Time `json:"lastUsedAt"  orm:"last_used_at" ` //
	CreatedAt   time.Time `json:"createdAt"   orm:"created_at"   ` //
	UpdatedAt   time.Time `json:"updatedAt"   orm:"updated_at"   ` //
	DeletedAt   time.Time `json:"deletedAt"   orm:"deleted_at"   ` //
}
