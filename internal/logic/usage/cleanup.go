package usage

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/dao"
)

const (
	// retentionCheckTick 是保留策略的巡检间隔。每次巡检只判断「今天这个时刻是否已执行过」，
	// 因此进程在计划时刻之后启动时，也能在下一个巡检周期内补跑。
	retentionCheckTick = 10 * time.Minute
	// retentionRunHour/retentionRunMinute 是每日清理时刻，使用进程本地时区
	// （由 config.setStorageTimezone 固定为 Asia/Shanghai，与库内 created_at 的墙钟口径一致）。
	retentionRunHour   = 3
	retentionRunMinute = 30
	// cleanupBatchSize 是单批删除行数：分批是为了避免一条 DELETE 长时间持有行锁与 undo。
	cleanupBatchSize = 1000
	// cleanupBatchPause 是批与批之间的停顿，给磁盘与其他写入留出间隙。
	cleanupBatchPause = 100 * time.Millisecond
	// cleanupMaxBatches 限制单轮最多执行的批数（默认 50 万行），
	// 避免首次开启保留策略时一次清理长时间占用数据库；余额留到下一轮继续。
	cleanupMaxBatches = 500
)

// expiredLogID 只用于承载待清理明细的主键，避免把整行读进内存。
type expiredLogID struct {
	Id uint64 `orm:"id"`
}

// StartRetentionCleanup 启动按保留窗口清理历史使用明细的后台任务。
// retentionDays <= 0 表示关闭清理（默认），此时不删除任何明细：
// 本项目的消费统计（仪表盘、用户用量、榜单）都实时聚合自 usage_logs，
// 删除明细会同时缩短历史统计的可查询窗口，必须由部署方显式开启。
func (s *sUsage) StartRetentionCleanup(ctx context.Context, retentionDays int) {
	if retentionDays <= 0 {
		g.Log().Infof(ctx, "usage retention cleanup disabled (USAGE_RETENTION_DAYS=%d)", retentionDays)
		return
	}
	go func() {
		var lastRun time.Time
		ticker := time.NewTicker(retentionCheckTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if !retentionDue(now, lastRun) {
					continue
				}
				lastRun = now
				s.cleanupExpiredLogs(ctx, retentionDays)
			}
		}
	}()
}

// retentionScheduledAt 返回 now 所在自然日的计划执行时刻。
func retentionScheduledAt(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), retentionRunHour, retentionRunMinute, 0, 0, now.Location())
}

// retentionDue 判断本轮巡检是否应执行清理：已过今日计划时刻，且本进程今日尚未执行。
// 用进程内状态而非持久化标记：计划时刻进程未运行时，启动后能在下个巡检周期补跑；
// 同一天重复执行也不会删到多余数据（此时只剩一次空查询）。
func retentionDue(now, lastRun time.Time) bool {
	scheduled := retentionScheduledAt(now)
	return !now.Before(scheduled) && lastRun.Before(scheduled)
}

// retentionCutoff 返回保留窗口的下界：本地时区当天 0 点回推 retentionDays 天。
// 返回 UTC 交给带 loc=Asia/Shanghai 的连接转换，与库内墙钟时间对齐。
func retentionCutoff(now time.Time, retentionDays int) time.Time {
	return startOfDay(now).AddDate(0, 0, -retentionDays).UTC()
}

func (s *sUsage) cleanupExpiredLogs(ctx context.Context, retentionDays int) {
	cutoff := retentionCutoff(time.Now(), retentionDays)
	deleted, err := s.CleanupBefore(ctx, cutoff, cleanupBatchSize, cleanupBatchPause, cleanupMaxBatches)
	if err != nil {
		g.Log().Warningf(ctx, "usage retention cleanup failed (before=%s): %v", cutoff.Format(logTimeLayout), err)
		return
	}
	if deleted == 0 {
		g.Log().Debugf(ctx, "usage retention cleanup found nothing to delete (before=%s)", cutoff.Format(logTimeLayout))
		return
	}
	g.Log().Infof(ctx, "usage retention cleanup deleted %d rows older than %s (USAGE_RETENTION_DAYS=%d)",
		deleted, cutoff.Format(logTimeLayout), retentionDays)
}

// CleanupBefore 分批删除 created_at 早于 cutoff 的使用明细，返回实际删除的行数。
// 每批先按 idx_usage_logs_created 取一批主键（覆盖索引，无需回表），再按主键删除，
// 因此单批上限由主键集合决定，不依赖 ORM 是否会把 Limit 拼接到 DELETE 语句上
// —— 若未拼接，一条 Delete 会清空整表。
func (s *sUsage) CleanupBefore(ctx context.Context, cutoff time.Time, batchSize int, pause time.Duration, maxBatches int) (int64, error) {
	if batchSize <= 0 || maxBatches <= 0 {
		return 0, nil
	}
	columns := dao.UsageLogs.Columns()
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		rows := make([]expiredLogID, 0, batchSize)
		if err := dao.UsageLogs.Ctx(ctx).
			Fields(columns.Id).
			WhereLT(columns.CreatedAt, cutoff).
			Limit(batchSize).
			Scan(&rows); err != nil {
			return total, gerror.Wrap(err, "load expired usage log ids")
		}
		if len(rows) == 0 {
			return total, nil
		}
		ids := make([]uint64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.Id)
		}
		result, err := dao.UsageLogs.Ctx(ctx).WhereIn(columns.Id, ids).Delete()
		if err != nil {
			return total, gerror.Wrap(err, "delete expired usage logs")
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr == nil {
			total += affected
		} else {
			total += int64(len(ids))
		}
		// 不足一批说明已经删到窗口边界，本轮结束。
		if len(rows) < batchSize {
			return total, nil
		}
		if pause <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(pause):
		}
	}
	return total, nil
}
