package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
)

// configVersionSQL 是读版本号的唯一语句，cluster_state 是单行表（id 固定 1）。
const configVersionSQL = "SELECT config_version FROM cluster_state WHERE id = 1"

// ConfigVersion 返回当前全局配置版本号。
func (s *Service) ConfigVersion(ctx context.Context) (int64, error) {
	value, err := s.database.GetValue(ctx, configVersionSQL)
	if err != nil {
		return 0, gerror.Wrap(err, "read cluster config version")
	}
	if value.IsNil() {
		return 0, gerror.New("cluster: cluster_state row is missing")
	}
	return value.Int64(), nil
}

// Mark 递增全局配置版本并记录一条变更事件。
//
// 所有配置写入路径（管理端保存、后台巡检落库）在写成功后调用它，子节点据此
// 感知「配置变了」。kind/ref 只用于排查，不参与一致性判定。
func (s *Service) Mark(ctx context.Context, kind, ref string) error {
	if !s.Enabled() || s.IsWorker() {
		return nil
	}
	err := s.database.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		bound := tx.Ctx(ctx)
		if _, err := bound.Exec(
			"UPDATE cluster_state SET config_version = config_version + 1 WHERE id = 1"); err != nil {
			return err
		}
		if _, err := bound.Exec(
			"INSERT INTO cluster_config_events(kind, ref) VALUES(?, ?)", kind, truncate(ref, 64)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return gerror.Wrap(err, "mark cluster config change")
	}
	s.TriggerPush()
	return nil
}

// fingerprintTables 是需要纳入指纹巡检的配置表，与快照下发范围保持一致。
var fingerprintTables = []string{
	"channels", "channel_credentials", "channel_models", "channel_model_credentials",
	"channel_groups", "channel_group_members", "channel_types", "model_prices",
	"model_price_rules", "price_sources", "system_settings", "api_keys",
	"api_key_models", "api_key_channel_groups", "api_key_channel_credentials", "users",
}

// Fingerprint 汇总每张配置表的行数与最后更新时间，得到可比较的指纹字符串。
//
// 这条兜底路径解决两类问题：新代码漏调用 Mark；运维直接改库。两者都不会递增
// 版本号，子节点就会长期停在旧配置上。
func (s *Service) Fingerprint(ctx context.Context) (string, error) {
	parts := make([]string, 0, len(fingerprintTables))
	for _, table := range fingerprintTables {
		sql := fmt.Sprintf(
			"SELECT CONCAT(COUNT(*), ':', COALESCE(UNIX_TIMESTAMP(MAX(updated_at)), -1)) FROM %s", table)
		value, err := s.database.GetValue(ctx, sql)
		if err != nil {
			return "", gerror.Wrapf(err, "fingerprint table %s", table)
		}
		parts = append(parts, table+"="+value.String())
	}
	return strings.Join(parts, "|"), nil
}

// WatchFingerprint 比较指纹与上次记录，变化时递增版本并返回 true。
// 首次调用只建立基线，不触发递增（避免重启后无意义的推送）。
func (s *Service) WatchFingerprint(ctx context.Context) (bool, error) {
	fingerprint, err := s.Fingerprint(ctx)
	if err != nil {
		return false, err
	}
	s.fingerprintMu.Lock()
	previous := s.fingerprintValue
	s.fingerprintValue = fingerprint
	s.fingerprintMu.Unlock()
	if previous == "" || previous == fingerprint {
		return false, nil
	}
	if err = s.Mark(ctx, "fingerprint", ""); err != nil {
		return false, err
	}
	return true, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
