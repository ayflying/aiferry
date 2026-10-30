package cluster

import (
	"context"
	"reflect"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
)

// buildSnapshot 从主库读取全量配置。查询故意使用 Unscoped：
// 软删除行也要带上，子节点镜像据此才能感知「主节点删掉了这一行」。
func buildSnapshot(ctx context.Context, version int64) (*Snapshot, error) {
	snapshot := &Snapshot{
		Version:     version,
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
	loaders := []struct {
		name string
		load func() error
	}{
		{"channels", func() error { return dao.Channels.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.Channels) }},
		{"channel_credentials", func() error {
			return dao.ChannelCredentials.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ChannelCredentials)
		}},
		{"channel_models", func() error {
			return dao.ChannelModels.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ChannelModels)
		}},
		{"channel_model_credentials", func() error {
			return dao.ChannelModelCredentials.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ChannelModelCredentials)
		}},
		{"channel_groups", func() error {
			return dao.ChannelGroups.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ChannelGroups)
		}},
		{"channel_group_members", func() error {
			return dao.ChannelGroupMembers.Ctx(ctx).Order("channel_group_id", "channel_id").
				Scan(&snapshot.ChannelGroupMembers)
		}},
		{"channel_types", func() error {
			return dao.ChannelTypes.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ChannelTypes)
		}},
		{"model_prices", func() error {
			return dao.ModelPrices.Ctx(ctx).Unscoped().Order("public_name").Scan(&snapshot.ModelPrices)
		}},
		{"model_price_rules", func() error {
			return dao.ModelPriceRules.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ModelPriceRules)
		}},
		{"price_sources", func() error {
			return dao.PriceSources.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.PriceSources)
		}},
		{"system_settings", func() error {
			return dao.SystemSettings.Ctx(ctx).Order("setting_key").Scan(&snapshot.SystemSettings)
		}},
		{"api_keys", func() error {
			return dao.ApiKeys.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.ApiKeys)
		}},
		{"api_key_models", func() error {
			return dao.ApiKeyModels.Ctx(ctx).Order("api_key_id", "model_name").Scan(&snapshot.ApiKeyModels)
		}},
		{"api_key_channel_groups", func() error {
			return dao.ApiKeyChannelGroups.Ctx(ctx).Order("api_key_id", "channel_group_id").
				Scan(&snapshot.ApiKeyChannelGroups)
		}},
		{"api_key_channel_credentials", func() error {
			return dao.ApiKeyChannelCredentials.Ctx(ctx).Order("api_key_id", "channel_id").
				Scan(&snapshot.ApiKeyChannelCredentials)
		}},
		{"users", func() error {
			return dao.Users.Ctx(ctx).Unscoped().Order("id").Scan(&snapshot.Users)
		}},
	}
	for _, loader := range loaders {
		if err := loader.load(); err != nil {
			return nil, gerror.Wrapf(err, "load %s for cluster snapshot", loader.name)
		}
	}
	return snapshot, nil
}

// entityRows 把实体切片转换成「列名 -> 值」的行集合，并剔除节点本地列。
//
// 为什么不直接用 gconv.Map：entity 上的 json tag 是驼峰命名，与数据库列名不一致；
// 这里以 orm tag 为准，避免依赖 gconv 的 tag 优先级配置。
func entityRows[T any](items []T, local []string) []map[string]any {
	drop := make(map[string]struct{}, len(local))
	for _, column := range local {
		drop[column] = struct{}{}
	}
	rows := make([]map[string]any, 0, len(items))
	for index := range items {
		rows = append(rows, entityRow(&items[index], drop))
	}
	return rows
}

func entityRow(item any, drop map[string]struct{}) map[string]any {
	value := reflect.ValueOf(item).Elem()
	structType := value.Type()
	row := make(map[string]any, structType.NumField())
	for index := 0; index < structType.NumField(); index++ {
		column := structType.Field(index).Tag.Get("orm")
		if column == "" || column == "-" {
			continue
		}
		if _, skip := drop[column]; skip {
			continue
		}
		field := value.Field(index)
		// 零值时间必须落成 SQL NULL：GoFrame 的软删除依赖 deleted_at IS NULL 判断，
		// 写成 0001-01-01 会让镜像里所有行都被当成已删除。
		// 这里用 Raw 而不是 nil：nil 会被 ORM 的数据过滤当成「未提供」而填成 0。
		if field.Type() == reflect.TypeOf(time.Time{}) {
			if field.Interface().(time.Time).IsZero() {
				row[column] = gdb.Raw("NULL")
				continue
			}
			row[column] = field.Interface()
			continue
		}
		row[column] = field.Interface()
	}
	return row
}
