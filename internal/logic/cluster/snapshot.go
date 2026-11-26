package cluster

import (
	"github.com/yunloli/aiferry/internal/model/entity"
	"time"
)

// UserChannelGroup 是快照协议中的用户分组授权关系（非生成数据库实体）。
type UserChannelGroup struct {
	UserId         uint64 `json:"userId" orm:"user_id"`
	ChannelGroupId uint64 `json:"channelGroupId" orm:"channel_group_id"`
}

// SnapshotAPIKey 保留限额的 SQL NULL 语义；nil 表示不限额，显式 0 表示零限额。
// 使用平铺 DTO 而非生成 entity，避免 ORM/JSON 和镜像字段反射忽略嵌入字段。
type SnapshotAPIKey struct {
	Id               uint64    `json:"id" orm:"id"`
	UserId           uint64    `json:"userId" orm:"user_id"`
	Name             string    `json:"name" orm:"name"`
	KeyPrefix        string    `json:"keyPrefix" orm:"key_prefix"`
	KeyHash          string    `json:"keyHash" orm:"key_hash"`
	KeyCipher        string    `json:"keyCipher" orm:"key_cipher"`
	Status           int       `json:"status" orm:"status"`
	SpendLimit       *float64  `json:"spendLimit" orm:"spend_limit"`
	DailySpendLimit  *float64  `json:"dailySpendLimit" orm:"daily_spend_limit"`
	SpentAmount      float64   `json:"spentAmount" orm:"spent_amount"`
	DailySpentAmount float64   `json:"dailySpentAmount" orm:"daily_spent_amount"`
	DailySpendDate   time.Time `json:"dailySpendDate" orm:"daily_spend_date"`
	ExpiresAt        time.Time `json:"expiresAt" orm:"expires_at"`
	LastUsedAt       time.Time `json:"lastUsedAt" orm:"last_used_at"`
	CreatedAt        time.Time `json:"createdAt" orm:"created_at"`
	UpdatedAt        time.Time `json:"updatedAt" orm:"updated_at"`
	DeletedAt        time.Time `json:"deletedAt" orm:"deleted_at"`
}

// SnapshotModelMetadata 是模型元数据的镜像协议，Metadata 保存原始 JSON 文本，
// 不经生成 entity 的标量转换，保留嵌套 null、false 和零值。
type SnapshotModelMetadata struct {
	ChannelId uint64    `json:"channelId" orm:"channel_id"`
	ModelName string    `json:"modelName" orm:"model_name"`
	Metadata  string    `json:"metadata" orm:"metadata"`
	CreatedAt time.Time `json:"createdAt" orm:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" orm:"updated_at"`
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
	ChannelModels            []SnapshotChannelModel            `json:"channelModels"`
	ChannelModelCredentials  []entity.ChannelModelCredentials  `json:"channelModelCredentials"`
	ChannelGroups            []entity.ChannelGroups            `json:"channelGroups"`
	ChannelGroupMembers      []entity.ChannelGroupMembers      `json:"channelGroupMembers"`
	ChannelTypes             []entity.ChannelTypes             `json:"channelTypes"`
	ModelMetadata            []SnapshotModelMetadata           `json:"modelMetadata"`
	ModelPrices              []SnapshotModelPrice              `json:"modelPrices"`
	ModelPriceRules          []entity.ModelPriceRules          `json:"modelPriceRules"`
	PriceSources             []entity.PriceSources             `json:"priceSources"`
	SystemSettings           []entity.SystemSettings           `json:"systemSettings"`
	ApiKeys                  []SnapshotAPIKey                  `json:"apiKeys"`
	ApiKeyModels             []entity.ApiKeyModels             `json:"apiKeyModels"`
	ApiKeyChannelGroups      []entity.ApiKeyChannelGroups      `json:"apiKeyChannelGroups"`
	ApiKeyChannelCredentials []entity.ApiKeyChannelCredentials `json:"apiKeyChannelCredentials"`
	Users                    []entity.Users                    `json:"users"`
	UserChannelGroups        []UserChannelGroup                `json:"userChannelGroups"`
}
