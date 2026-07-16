// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelModels is the golang structure of table channel_models for DAO operations like Where/Data.
type ChannelModels struct {
	g.Meta            `orm:"table:channel_models, do:true"`
	Id                any //
	ChannelId         any //
	PublicName        any //
	UpstreamName      any //
	Discovered        any //
	Enabled           any //
	InputPrice        any //
	CachedInputPrice  any //
	OutputPrice       any //
	LastTestEndpoint  any //
	LastTestStatus    any //
	LastTestLatencyMs any //
	LastTestError     any //
	LastTestAt        any //
	CreatedAt         any //
	UpdatedAt         any //
	DeletedAt         any //
}
