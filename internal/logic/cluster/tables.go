package cluster

import (
	"github.com/yunloli/aiferry/internal/model/entity"
)

// tableSpec 描述一张镜像表如何从快照写入子节点。
//
// 字段含义：
//   - name：子节点 SQLite 中的表名，与主库同名；
//   - keys：冲突判定的列，写入用 upsert 语义；
//   - local：节点本地维护的列，快照不会覆盖（健康分、余额、消耗累计等）；
//   - full：true 表示该表没有本地列，直接整表替换，避免 IN 列表过长。
type tableSpec struct {
	name  string
	keys  []string
	local []string
	full  bool
	rows  func(*Snapshot) []map[string]any
}

func table[T any](name string, keys []string, local []string, full bool, pick func(*Snapshot) []T) tableSpec {
	return tableSpec{
		name:  name,
		keys:  keys,
		local: local,
		full:  full,
		rows: func(snapshot *Snapshot) []map[string]any {
			return entityRows(pick(snapshot), local)
		},
	}
}

// snapshotTables 是镜像表的完整清单，顺序即应用顺序（先父表后子表，便于阅读）。
//
// 本地列的选择原则：凡是「由本节点的流量与失败累积出来」的数值都不覆盖，
// 否则每次同步都会把节点自己的健康判断清零；反之凡是「由管理端决定」的状态
// （渠道启用状态、凭据状态）都以主节点为准。
var snapshotTables = []tableSpec{
	table("channels", []string{"id"}, nil, false,
		func(s *Snapshot) []entity.Channels { return s.Channels }),
	table("channel_credentials", []string{"id"}, nil, false,
		func(s *Snapshot) []entity.ChannelCredentials { return s.ChannelCredentials }),
	table("channel_models", []string{"id"},
		[]string{"health_score", "auto_disabled_at", "auto_disabled_reason", "auto_disabled_source"}, false,
		func(s *Snapshot) []entity.ChannelModels { return s.ChannelModels }),
	table("channel_model_credentials", []string{"id"},
		[]string{"health_score", "cooldown_until", "last_error"}, false,
		func(s *Snapshot) []entity.ChannelModelCredentials { return s.ChannelModelCredentials }),
	table("channel_groups", []string{"id"}, nil, true,
		func(s *Snapshot) []entity.ChannelGroups { return s.ChannelGroups }),
	table("channel_group_members", []string{"channel_group_id", "channel_id"}, nil, true,
		func(s *Snapshot) []entity.ChannelGroupMembers { return s.ChannelGroupMembers }),
	table("channel_types", []string{"id"}, nil, true,
		func(s *Snapshot) []entity.ChannelTypes { return s.ChannelTypes }),
	table("model_prices", []string{"public_name"}, nil, true,
		func(s *Snapshot) []entity.ModelPrices { return s.ModelPrices }),
	table("model_price_rules", []string{"id"}, nil, true,
		func(s *Snapshot) []entity.ModelPriceRules { return s.ModelPriceRules }),
	table("price_sources", []string{"id"}, nil, true,
		func(s *Snapshot) []entity.PriceSources { return s.PriceSources }),
	table("system_settings", []string{"setting_key"}, nil, true,
		func(s *Snapshot) []entity.SystemSettings { return s.SystemSettings }),
	// 账务字段（余额、累计消耗）由主节点作为唯一权威值下发。
	// 子节点在两次同步之间各自记账，最多滞后一个同步周期，这正是「允许一个
	// 周期超支」的取舍；若把账务留在本地，主节点充值和管理端调整就永远传不下去。
	table("api_keys", []string{"id"}, nil, false,
		func(s *Snapshot) []entity.ApiKeys { return s.ApiKeys }),
	table("api_key_models", []string{"api_key_id", "model_name"}, nil, true,
		func(s *Snapshot) []entity.ApiKeyModels { return s.ApiKeyModels }),
	table("api_key_channel_groups", []string{"api_key_id", "channel_group_id"}, nil, true,
		func(s *Snapshot) []entity.ApiKeyChannelGroups { return s.ApiKeyChannelGroups }),
	table("api_key_channel_credentials", []string{"api_key_id", "channel_id"}, nil, true,
		func(s *Snapshot) []entity.ApiKeyChannelCredentials { return s.ApiKeyChannelCredentials }),
	table("users", []string{"id"}, nil, false,
		func(s *Snapshot) []entity.Users { return s.Users }),
}
