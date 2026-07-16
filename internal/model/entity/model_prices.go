// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"github.com/gogf/gf/v2/os/gtime"
)

// ModelPrices is the golang structure for table model_prices.
type ModelPrices struct {
	PublicName       string      `json:"publicName"       orm:"public_name"        ` //
	InputPrice       float64     `json:"inputPrice"       orm:"input_price"        ` //
	CachedInputPrice float64     `json:"cachedInputPrice" orm:"cached_input_price" ` //
	OutputPrice      float64     `json:"outputPrice"      orm:"output_price"       ` //
	CreatedAt        *gtime.Time `json:"createdAt"        orm:"created_at"         ` //
	UpdatedAt        *gtime.Time `json:"updatedAt"        orm:"updated_at"         ` //
	DeletedAt        *gtime.Time `json:"deletedAt"        orm:"deleted_at"         ` //
}
