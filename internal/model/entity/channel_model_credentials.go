// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"github.com/gogf/gf/v2/os/gtime"
)

// ChannelModelCredentials is the golang structure for table channel_model_credentials.
type ChannelModelCredentials struct {
	Id                  uint64      `json:"id"                  orm:"id"                    description:""`                   //
	ChannelId           uint64      `json:"channelId"           orm:"channel_id"            description:""`                   //
	ChannelModelId      uint64      `json:"channelModelId"      orm:"channel_model_id"      description:""`                   //
	ChannelCredentialId uint64      `json:"channelCredentialId" orm:"channel_credential_id" description:""`                   //
	HealthScore         int         `json:"healthScore"         orm:"health_score"          description:"组合健康分（0-100），默认100"` // 组合健康分（0-100），默认100
	CooldownUntil       *gtime.Time `json:"cooldownUntil"       orm:"cooldown_until"        description:"冷却截至时间，NULL 表示未冷却"`  // 冷却截至时间，NULL 表示未冷却
	LastError           string      `json:"lastError"           orm:"last_error"            description:"最近一次错误摘要"`           // 最近一次错误摘要
	CreatedAt           *gtime.Time `json:"createdAt"           orm:"created_at"            description:""`                   //
	UpdatedAt           *gtime.Time `json:"updatedAt"           orm:"updated_at"            description:""`                   //
}
