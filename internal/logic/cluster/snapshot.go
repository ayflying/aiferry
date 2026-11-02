package cluster

import (
	"github.com/yunloli/aiferry/internal/model/entity"
)

// UserChannelGroup 是快照协议中的用户分组授权关系（非生成数据库实体）。
type UserChannelGroup struct {
	UserId         uint64 `json:"userId" orm:"user_id"`
	ChannelGroupId uint64 `json:"channelGroupId" orm:"channel_group_id"`
}

// Snapshot 是主节点下发给子节点的全量配置快照。
//
// 设计取舍：整包下发而不是增量补丁。配置表规模是「数十个渠道 + 千级模型」，
// gzip 后为百 KB 级，整包能保证任意时刻子节点状态收敛到主节点，且天然幂等；
// 增量补丁需要维护游标与补偿逻辑，收益不足以抵消复杂度。
type Snapshot struct {
	// Version 是主节点的全局配置版本号，子节点据此判断是否需要重新拉取。
	Version int64 `json:"version"`
	// GeneratedAt 是快照生成时间，仅用于排查，不参与一致性判断。
	GeneratedAt string `json:"generatedAt"`

	Channels                 []entity.Channels                 `json:"channels"`
	ChannelCredentials       []entity.ChannelCredentials       `json:"channelCredentials"`
	ChannelModels            []entity.ChannelModels            `json:"channelModels"`
	ChannelModelCredentials  []entity.ChannelModelCredentials  `json:"channelModelCredentials"`
	ChannelGroups            []entity.ChannelGroups            `json:"channelGroups"`
	ChannelGroupMembers      []entity.ChannelGroupMembers      `json:"channelGroupMembers"`
	ChannelTypes             []entity.ChannelTypes             `json:"channelTypes"`
	ModelPrices              []entity.ModelPrices              `json:"modelPrices"`
	ModelPriceRules          []entity.ModelPriceRules          `json:"modelPriceRules"`
	PriceSources             []entity.PriceSources             `json:"priceSources"`
	SystemSettings           []entity.SystemSettings           `json:"systemSettings"`
	ApiKeys                  []entity.ApiKeys                  `json:"apiKeys"`
	ApiKeyModels             []entity.ApiKeyModels             `json:"apiKeyModels"`
	ApiKeyChannelGroups      []entity.ApiKeyChannelGroups      `json:"apiKeyChannelGroups"`
	ApiKeyChannelCredentials []entity.ApiKeyChannelCredentials `json:"apiKeyChannelCredentials"`
	Users                    []entity.Users                    `json:"users"`
	UserChannelGroups        []UserChannelGroup                `json:"userChannelGroups"`
}
