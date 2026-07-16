// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKeyModels is the golang structure for table api_key_models.
type ApiKeyModels struct {
	ApiKeyId  uint64    `json:"apiKeyId"  orm:"api_key_id" ` //
	ModelName string    `json:"modelName" orm:"model_name" ` //
	CreatedAt time.Time `json:"createdAt" orm:"created_at" ` //
}
