// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKeyChannelCredentials is the golang structure for table api_key_channel_credentials.
type ApiKeyChannelCredentials struct {
	ApiKeyId            uint64    `json:"apiKeyId"            orm:"api_key_id"            description:""` //
	ChannelId           uint64    `json:"channelId"           orm:"channel_id"            description:""` //
	ChannelCredentialId uint64    `json:"channelCredentialId" orm:"channel_credential_id" description:""` //
	CreatedAt           time.Time `json:"createdAt"           orm:"created_at"            description:""` //
	UpdatedAt           time.Time `json:"updatedAt"           orm:"updated_at"            description:""` //
}
