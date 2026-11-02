package cluster

import (
	"context"
	"reflect"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
)

// buildSnapshot 读取当前服务数据库的完整配置，包括软删除记录。
// 显式绑定数据库防止生成 DAO 的默认连接与当前服务连接不一致。
func buildSnapshot(ctx context.Context, database gdb.DB, version int64) (*Snapshot, error) {
	snapshot := &Snapshot{Version: version, GeneratedAt: time.Now().Format(time.RFC3339)}
	loaders := []struct {
		name   string
		order  string
		target any
	}{
		{"channels", "id", &snapshot.Channels},
		{"channel_credentials", "id", &snapshot.ChannelCredentials},
		{"channel_models", "id", &snapshot.ChannelModels},
		{"channel_model_credentials", "id", &snapshot.ChannelModelCredentials},
		{"channel_groups", "id", &snapshot.ChannelGroups},
		{"channel_group_members", "channel_group_id, channel_id", &snapshot.ChannelGroupMembers},
		{"channel_types", "id", &snapshot.ChannelTypes},
		{"model_prices", "public_name", &snapshot.ModelPrices},
		{"model_price_rules", "id", &snapshot.ModelPriceRules},
		{"price_sources", "id", &snapshot.PriceSources},
		{"system_settings", "setting_key", &snapshot.SystemSettings},
		{"api_keys", "id", &snapshot.ApiKeys},
		{"api_key_models", "api_key_id, model_name", &snapshot.ApiKeyModels},
		{"api_key_channel_groups", "api_key_id, channel_group_id", &snapshot.ApiKeyChannelGroups},
		{"api_key_channel_credentials", "api_key_id, channel_id", &snapshot.ApiKeyChannelCredentials},
		{"users", "id", &snapshot.Users},
		{"user_channel_groups", "user_id, channel_group_id", &snapshot.UserChannelGroups},
	}
	// 全部配置读取共享一个事务视图；MySQL 默认 REPEATABLE READ 下，
	// 用户余额与密钥消耗不会跨越并发账务提交读出不同批次的数据。
	err := database.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		for _, loader := range loaders {
			if err := tx.Model(loader.name).Ctx(ctx).Unscoped().Order(loader.order).Scan(loader.target); err != nil {
				return gerror.Wrapf(err, "load %s for cluster snapshot", loader.name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, gerror.Wrap(err, "read consistent cluster snapshot")
	}
	return snapshot, nil
}

// entityRows 用 orm tag 转换数据库字段，过滤节点本地维护列。
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
		// 指针数字的 nil 必须显式写 NULL，不能被 ORM 当作未提供字段过滤掉。
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				row[column] = gdb.Raw("NULL")
			} else {
				row[column] = field.Elem().Interface()
			}
			continue
		}
		// 零值时间必须是 SQL NULL，否则镜像会被 ORM 误判为软删除。
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
