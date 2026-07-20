// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// RedemptionCodes is the golang structure for table redemption_codes.
type RedemptionCodes struct {
	Id               uint64    `json:"id"               orm:"id"                  description:""` //
	Name             string    `json:"name"             orm:"name"                description:""` //
	Code             string    `json:"code"             orm:"code"                description:""` //
	Amount           float64   `json:"amount"           orm:"amount"              description:""` //
	ExpiresAt        time.Time `json:"expiresAt"        orm:"expires_at"          description:""` //
	RedeemedByUserId uint64    `json:"redeemedByUserId" orm:"redeemed_by_user_id" description:""` //
	RedeemedAt       time.Time `json:"redeemedAt"       orm:"redeemed_at"         description:""` //
	CreatedByUserId  uint64    `json:"createdByUserId"  orm:"created_by_user_id"  description:""` //
	CreatedAt        time.Time `json:"createdAt"        orm:"created_at"          description:""` //
	UpdatedAt        time.Time `json:"updatedAt"        orm:"updated_at"          description:""` //
}
