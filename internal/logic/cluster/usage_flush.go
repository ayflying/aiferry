package cluster

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// FlushUsage 把本地累积的用量明细批量回传给主节点。
//
// 只有子节点需要回传；主节点自己的用量在记账时就已入主库。
// 传输成功后才删除本地行，失败自动重试下一轮，不会因为主节点临时不可用丢账。
func (s *Service) FlushUsage(ctx context.Context) error {
	if !s.Enabled() || !s.IsWorker() {
		return nil
	}
	rows, err := s.pendingUsage(ctx, s.config.Cluster.BatchSize)
	if err != nil || len(rows) == 0 {
		return err
	}
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	batch := usageBatch{BatchID: newBatchID(), Rows: usageRows(rows)}
	if batch.BatchID == "" {
		return gerror.New("cluster: generate usage batch id")
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return gerror.Wrap(err, "encode usage batch")
	}
	response, err := s.call(ctx, s.config.Cluster.MasterURL, "/usage", body, contentTypeJSON)
	if err != nil {
		return gerror.Wrap(err, "post usage batch")
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && s.logger != nil {
			s.logger.Warningf(ctx, "cluster: close usage response: %v", closeErr)
		}
	}()
	if response.StatusCode != 200 {
		message, _ := readLimited(response)
		return gerror.Newf("cluster: usage batch rejected with %d: %s", response.StatusCode, truncate(message, 200))
	}
	var ack usageAck
	if err = decodeGzipJSON(response, &ack); err != nil {
		return gerror.Wrap(err, "decode usage ack")
	}
	// 主节点已确认收下，这里才删本地行：宁可重复回传（主节点按批次号幂等），
	// 也不能在未确认前删掉唯一的账务凭据。
	if err = s.deleteUsageRows(ctx, ids); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Infof(ctx, "cluster: flushed %d usage rows (accepted %d, replayed %t)", len(ids), ack.Accepted, ack.Replayed)
	}
	return nil
}

// pendingUsage 读取最早的一批待回传明细。
func (s *Service) pendingUsage(ctx context.Context, limit int) ([]entity.UsageLogs, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []entity.UsageLogs
	if err := s.database.Model("usage_logs").Ctx(ctx).OrderAsc("id").Limit(limit).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "read pending usage rows")
	}
	return rows, nil
}

// deleteUsageRows 在主节点确认后清理本地明细。
func (s *Service) deleteUsageRows(ctx context.Context, ids []uint64) error {
	const chunk = 200
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		if _, err := s.database.Model("usage_logs").Ctx(ctx).WhereIn("id", part).Delete(); err != nil {
			return gerror.Wrap(err, "delete flushed usage rows")
		}
	}
	return nil
}

// usageLoop 按 FlushInterval 周期回传用量，并在超过软上限时告警。
func (s *Service) usageLoop(ctx context.Context) {
	interval := s.config.Cluster.FlushInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.FlushUsage(ctx); err != nil && s.logger != nil {
				s.logger.Warningf(ctx, "cluster: flush usage: %v", err)
			}
			s.warnUsageBacklog(ctx)
		}
	}
}

// warnUsageBacklog 在待回传明细长期堆积时提醒运维检查与主节点的连通性。
func (s *Service) warnUsageBacklog(ctx context.Context) {
	limit := s.config.Cluster.OutboxMaxRows
	if limit <= 0 || s.logger == nil {
		return
	}
	pending := s.pendingRows(ctx)
	if pending >= int64(limit) {
		s.logger.Warningf(ctx, "cluster: %d usage rows pending, master may be unreachable", pending)
	}
}
