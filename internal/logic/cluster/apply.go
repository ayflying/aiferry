package cluster

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
)

// mirrorInsertChunk 是单条 upsert 语句携带的行数上限，控制 SQL 长度与参数个数。
const mirrorInsertChunk = 200

// applySnapshot 把整包快照写进子节点内置 SQLite，整个过程在一个事务里完成：
// 中途失败时子节点仍是上一份完整配置，不会出现「删了旧的还没写新的」的中间态。
func applySnapshot(ctx context.Context, database gdb.DB, snapshot *Snapshot) error {
	if database == nil {
		return gerror.New("cluster: worker database is not initialized")
	}
	return database.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		for _, spec := range snapshotTables {
			rows := spec.rows(snapshot)
			if spec.full {
				if _, err := tx.Ctx(ctx).Exec("DELETE FROM " + spec.name); err != nil {
					return gerror.Wrapf(err, "clear mirror table %s", spec.name)
				}
			}
			if err := upsertRows(ctx, tx, spec, rows); err != nil {
				return err
			}
			if !spec.full {
				if err := pruneRows(ctx, tx, spec, rows); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// upsertRows 按冲突列执行 upsert。
//
// 必须用 Save 而不是 Insert：GoFrame 只有在 Save 语义下才会把 OnConflict 传给驱动，
// 用 Insert 会退化成普通插入，重复同步直接撞唯一键报错。
// 列白名单由 tableSpec.local 保证：本地列根本不在行数据里，既不会被覆盖，
// 也不会被写进新插入的行。
func upsertRows(ctx context.Context, tx gdb.TX, spec tableSpec, rows []map[string]any) error {
	for start := 0; start < len(rows); start += mirrorInsertChunk {
		end := start + mirrorInsertChunk
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		conflicts := make([]any, 0, len(spec.keys))
		for _, key := range spec.keys {
			conflicts = append(conflicts, key)
		}
		if _, err := tx.Ctx(ctx).Model(spec.name).Data(chunk).OnConflict(conflicts...).Save(); err != nil {
			return gerror.Wrapf(err, "upsert %d rows into %s", len(chunk), spec.name)
		}
	}
	return nil
}

// pruneRows 删除主节点已经不存在的行。上一轮配置里存在、本轮快照里消失的行，
// 意味着主节点已经删除或软删除，镜像里必须同步移除。
func pruneRows(ctx context.Context, tx gdb.TX, spec tableSpec, rows []map[string]any) error {
	key := spec.keys[0]
	if len(rows) == 0 {
		if _, err := tx.Ctx(ctx).Exec("DELETE FROM " + spec.name); err != nil {
			return gerror.Wrapf(err, "clear mirror table %s", spec.name)
		}
		return nil
	}
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		values = append(values, row[key])
	}
	const pruneChunk = 400
	for start := 0; start < len(values); start += pruneChunk {
		end := start + pruneChunk
		if end > len(values) {
			end = len(values)
		}
		chunk := values[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		statement := "DELETE FROM " + spec.name + " WHERE " + key + " NOT IN (" + placeholders + ")"
		if _, err := tx.Ctx(ctx).Exec(statement, chunk...); err != nil {
			return gerror.Wrapf(err, "prune stale rows from %s", spec.name)
		}
	}
	return nil
}
