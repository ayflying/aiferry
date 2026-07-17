// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ModelPrices is the golang structure for table model_prices.
type ModelPrices struct {
	PublicName       string    `json:"publicName"       orm:"public_name"        description:""` //
	InputPrice       float64   `json:"inputPrice"       orm:"input_price"        description:""` //
	CachedInputPrice float64   `json:"cachedInputPrice" orm:"cached_input_price" description:""` //
	OutputPrice      float64   `json:"outputPrice"      orm:"output_price"       description:""` //
	CreatedAt        time.Time `json:"createdAt"        orm:"created_at"         description:""` //
	UpdatedAt        time.Time `json:"updatedAt"        orm:"updated_at"         description:""` //
	DeletedAt        time.Time `json:"deletedAt"        orm:"deleted_at"         description:""` //
}
