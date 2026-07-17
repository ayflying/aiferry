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
	BillingMode      string    `json:"billingMode"      orm:"billing_mode"       description:""` //
	InputPrice       float64   `json:"inputPrice"       orm:"input_price"        description:""` //
	CachedInputPrice float64   `json:"cachedInputPrice" orm:"cached_input_price" description:""` //
	CacheWritePrice  float64   `json:"cacheWritePrice"  orm:"cache_write_price"  description:""` //
	OutputPrice      float64   `json:"outputPrice"      orm:"output_price"       description:""` //
	ImageInputPrice  float64   `json:"imageInputPrice"  orm:"image_input_price"  description:""` //
	AudioInputPrice  float64   `json:"audioInputPrice"  orm:"audio_input_price"  description:""` //
	AudioOutputPrice float64   `json:"audioOutputPrice" orm:"audio_output_price" description:""` //
	RequestPrice     float64   `json:"requestPrice"     orm:"request_price"      description:""` //
	CreatedAt        time.Time `json:"createdAt"        orm:"created_at"         description:""` //
	UpdatedAt        time.Time `json:"updatedAt"        orm:"updated_at"         description:""` //
	DeletedAt        time.Time `json:"deletedAt"        orm:"deleted_at"         description:""` //
}
