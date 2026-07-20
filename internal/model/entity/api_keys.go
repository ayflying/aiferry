// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKeys is the golang structure for table api_keys.
type ApiKeys struct {
	Id               uint64    `json:"id"               orm:"id"                 description:""` //
	UserId           uint64    `json:"userId"           orm:"user_id"            description:""` //
	Name             string    `json:"name"             orm:"name"               description:""` //
	KeyPrefix        string    `json:"keyPrefix"        orm:"key_prefix"         description:""` //
	KeyHash          string    `json:"keyHash"          orm:"key_hash"           description:""` //
	KeyCipher        string    `json:"keyCipher"        orm:"key_cipher"         description:""` //
	Status           int       `json:"status"           orm:"status"             description:""` //
	SpendLimit       float64   `json:"spendLimit"       orm:"spend_limit"        description:""` //
	DailySpendLimit  float64   `json:"dailySpendLimit"  orm:"daily_spend_limit"  description:""` //
	SpentAmount      float64   `json:"spentAmount"      orm:"spent_amount"       description:""` //
	DailySpentAmount float64   `json:"dailySpentAmount" orm:"daily_spent_amount" description:""` //
	DailySpendDate   time.Time `json:"dailySpendDate"   orm:"daily_spend_date"   description:""` //
	ExpiresAt        time.Time `json:"expiresAt"        orm:"expires_at"         description:""` //
	LastUsedAt       time.Time `json:"lastUsedAt"       orm:"last_used_at"       description:""` //
	CreatedAt        time.Time `json:"createdAt"        orm:"created_at"         description:""` //
	UpdatedAt        time.Time `json:"updatedAt"        orm:"updated_at"         description:""` //
	DeletedAt        time.Time `json:"deletedAt"        orm:"deleted_at"         description:""` //
}
