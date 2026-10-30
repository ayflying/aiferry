package cluster

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
)

// usageIngestChunk 控制单次 SQL 的行数，避免超长语句。
const usageIngestChunk = 200

// HandleUsageBody 解析子节点上行载荷后入账，供内部接口直接调用。
func (s *Service) HandleUsageBody(ctx context.Context, node *Node, body []byte) (*UsageAck, error) {
	var input usageBatch
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, gerror.Wrap(err, "decode usage batch")
	}
	return s.HandleUsage(ctx, node, input)
}

// HandleUsage 接收子节点回传的用量明细并完成记账。
//
// 幂等由两层保证：批次号唯一（同一批次重放直接返回），以及 request_id 唯一
// （跨批次重复的行不会二次入账）。只有真正新插入的行才会触发扣费，
// 因此子节点超时重试不会导致重复扣款。
func (s *Service) HandleUsage(ctx context.Context, node *Node, input usageBatch) (*usageAck, error) {
	if node == nil || node.Id == 0 {
		return nil, gerror.New("cluster: usage batch without node identity")
	}
	if len(input.Rows) == 0 {
		return &usageAck{}, nil
	}
	ack := &usageAck{}
	err := s.database.Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		replayed, err := s.usageBatchSeen(txCtx, tx, node.Id, input.BatchID)
		if err != nil {
			return err
		}
		if replayed {
			ack.Replayed = true
			ack.Accepted = len(input.Rows)
			return nil
		}
		rows, err := s.filterNewUsageRows(txCtx, tx, input.Rows)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			if err = s.insertUsageRows(txCtx, tx, node.Id, rows); err != nil {
				return err
			}
			if err = s.billUsageRows(txCtx, rows); err != nil {
				return err
			}
		}
		ack.Accepted = len(rows)
		return s.recordUsageBatch(txCtx, tx, node.Id, input.BatchID, len(input.Rows), len(rows))
	})
	if err != nil {
		return nil, err
	}
	return ack, nil
}

// usageBatchSeen 判断该批次是否已经入账。
func (s *Service) usageBatchSeen(ctx context.Context, tx gdb.TX, nodeID uint64, batchID string) (bool, error) {
	if batchID == "" {
		return false, nil
	}
	count, err := tx.Ctx(ctx).Model("cluster_usage_batches").
		Where("node_id", nodeID).
		Where("batch_id", batchID).
		Count()
	if err != nil {
		return false, gerror.Wrap(err, "check usage batch")
	}
	return count > 0, nil
}

// recordUsageBatch 记录批次号，作为重放判定的依据。
func (s *Service) recordUsageBatch(ctx context.Context, tx gdb.TX, nodeID uint64, batchID string, received, accepted int) error {
	if batchID == "" {
		return nil
	}
	// created_at 由数据库默认值生成，这里不手工写时间列。
	if _, err := tx.Ctx(ctx).Model("cluster_usage_batches").Data(gdb.Map{
		"node_id":  nodeID,
		"batch_id": batchID,
		"rows":     accepted,
	}).Insert(); err != nil {
		return gerror.Wrapf(err, "record usage batch (%d rows received)", received)
	}
	return nil
}

// filterNewUsageRows 过滤掉已经入过账的 request_id。
func (s *Service) filterNewUsageRows(ctx context.Context, tx gdb.TX, rows []map[string]any) ([]map[string]any, error) {
	requestIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if value, ok := row["request_id"].(string); ok && value != "" {
			requestIDs = append(requestIDs, value)
		}
	}
	if len(requestIDs) == 0 {
		return rows, nil
	}
	existing := make(map[string]struct{}, len(requestIDs))
	for start := 0; start < len(requestIDs); start += usageIngestChunk {
		end := start + usageIngestChunk
		if end > len(requestIDs) {
			end = len(requestIDs)
		}
		values, err := tx.Ctx(ctx).Model("usage_logs").
			Fields("request_id").
			WhereIn("request_id", requestIDs[start:end]).
			Array()
		if err != nil {
			return nil, gerror.Wrap(err, "check existing usage rows")
		}
		for _, value := range values {
			existing[value.String()] = struct{}{}
		}
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id, _ := row["request_id"].(string)
		if _, found := existing[id]; found {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered, nil
}

// insertUsageRows 按节点归属写入明细，忽略并发插入造成的唯一键冲突。
func (s *Service) insertUsageRows(ctx context.Context, tx gdb.TX, nodeID uint64, rows []map[string]any) error {
	for start := 0; start < len(rows); start += usageIngestChunk {
		end := start + usageIngestChunk
		if end > len(rows) {
			end = len(rows)
		}
		chunk := make([]map[string]any, 0, end-start)
		for _, row := range rows[start:end] {
			item := make(map[string]any, len(row)+1)
			for key, value := range row {
				item[key] = value
			}
			item["node_id"] = nodeID
			chunk = append(chunk, item)
		}
		if _, err := tx.Ctx(ctx).Model("usage_logs").Data(chunk).InsertIgnore(); err != nil {
			return gerror.Wrap(err, "insert usage rows")
		}
	}
	return nil
}

// billUsageRows 对新增明细执行扣费，复用与转发侧完全相同的记账实现。
func (s *Service) billUsageRows(ctx context.Context, rows []map[string]any) error {
	if s.billing == nil {
		return nil
	}
	for _, row := range rows {
		cost, ok := usageCostOf(row)
		if !ok {
			continue
		}
		userID := uintValueOf(row["user_id"])
		if userID > 0 {
			if err := s.billing.Debit(ctx, userID, cost); err != nil {
				// 扣费失败不阻断入账：明细已经落库，运维可以按记录人工处理，
				// 直接回滚会丢掉子节点已经删掉的唯一凭据。
				s.clusterLogger().Warningf(ctx, "cluster: debit user %d: %v", userID, err)
			}
		}
		if apiKeyID := uintValueOf(row["api_key_id"]); apiKeyID > 0 {
			if err := s.billing.AddSpend(ctx, apiKeyID, cost.InexactFloat64()); err != nil {
				s.clusterLogger().Warningf(ctx, "cluster: add spend for key %d: %v", apiKeyID, err)
			}
		}
	}
	return nil
}

// usageCostOf 取出行内应扣金额。
//
// 只有 billing_details_json 里 charged 为 true 的行才扣费：自有渠道等
// 只统计不扣费的行必须跳过，否则子节点的统计记录会被重复收费。
func usageCostOf(row map[string]any) (decimal.Decimal, bool) {
	amount, ok := floatValueOf(row["estimated_cost"])
	if !ok || amount <= 0 {
		return decimal.Zero, false
	}
	raw, _ := row["billing_details_json"].(string)
	if raw != "" {
		var details struct {
			Charged bool `json:"charged"`
		}
		if err := json.Unmarshal([]byte(raw), &details); err == nil && !details.Charged {
			return decimal.Zero, false
		}
	}
	return decimal.NewFromFloat(amount), true
}

// uintValueOf 把行内的数值列转成无符号整数，兼容驱动返回的各种数值类型。
func uintValueOf(value any) uint64 {
	parsed, ok := intValueOf(value)
	if !ok || parsed < 0 {
		return 0
	}
	return uint64(parsed)
}
