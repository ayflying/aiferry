// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

// ChannelModelCredentials is the golang structure of table channel_model_credentials for DAO operations like Where/Data.
type ChannelModelCredentials struct {
	g.Meta              `orm:"table:channel_model_credentials, do:true"`
	Id                  any         //
	ChannelId           any         //
	ChannelModelId      any         //
	ChannelCredentialId any         //
	HealthScore         any         // 组合健康分（0-100），默认100
	CooldownUntil       *gtime.Time // 冷却截至时间，NULL 表示未冷却
	LastError           any         // 最近一次错误摘要
	CreatedAt           *gtime.Time //
	UpdatedAt           *gtime.Time //
}
