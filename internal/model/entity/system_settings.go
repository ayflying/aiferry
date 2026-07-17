// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// SystemSettings is the golang structure for table system_settings.
type SystemSettings struct {
	SettingKey string    `json:"settingKey" orm:"setting_key" description:""` //
	ValueJson  string    `json:"valueJson"  orm:"value_json"  description:""` //
	CreatedAt  time.Time `json:"createdAt"  orm:"created_at"  description:""` //
	UpdatedAt  time.Time `json:"updatedAt"  orm:"updated_at"  description:""` //
}
