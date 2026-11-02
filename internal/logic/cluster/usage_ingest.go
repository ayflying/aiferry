package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
)

// HandleUsageBody 保留 JSON 数字精度，并拒绝未知顶层字段和尾随载荷。
func (s *Service) HandleUsageBody(ctx context.Context, node *Node, body []byte) (*UsageAck, error) {
	var input usageBatch
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, gerror.Wrap(err, "decode usage batch")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, gerror.New("cluster: trailing usage batch data")
	}
	return s.HandleUsage(ctx, node, input)
}

// HandleUsage 仅在整批明细和扣费均提交后 ACK。Accepted 始终是该批首次提交时
// 真正新增的行数（重放也返回原值），不是收到的行数；重复 request_id 已经持久化，
// 可以随本批确认。任何验证、写入、扣费或提交错误都不返回成功回执。
func (s *Service) HandleUsage(ctx context.Context, node *Node, input usageBatch) (*usageAck, error) {
	if node == nil || node.Id == 0 {
		return nil, gerror.New("cluster: usage batch without node identity")
	}
	rows, err := validateUsageBatch(input)
	if err != nil {
		return nil, err
	}
	ack := &usageAck{}
	err = s.database.Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		// 先通过唯一约束争抢批次。不能先做快照 SELECT：MySQL RR 下另一
		// 事务提交后，旧快照仍可能看不到批次，造成误判或重复结算。
		replayed, accepted, err := s.claimUsageBatch(txCtx, tx, node.Id, input.BatchID)
		if err != nil {
			return err
		}
		if replayed {
			ack.Replayed, ack.Accepted = true, accepted
			return nil
		}
		inserted, err := s.insertUsageRows(txCtx, tx, node.Id, rows)
		if err != nil {
			return err
		}
		if err = s.billUsageRows(txCtx, inserted); err != nil {
			return err
		}
		ack.Accepted = len(inserted)
		// 两张 wire 表无对应生成 DO；这里仅写内部固定列，绝不透传任意列。
		_, err = tx.Ctx(txCtx).Model("cluster_usage_batches").
			Where("node_id", node.Id).Where("batch_id", input.BatchID).
			Data(gdb.Map{"rows": ack.Accepted}).Update()
		return gerror.Wrap(err, "complete usage batch")
	})
	if err != nil {
		return nil, err
	}
	return ack, nil
}

func (s *Service) claimUsageBatch(ctx context.Context, tx gdb.TX, nodeID uint64, batchID string) (bool, int, error) {
	_, err := tx.Ctx(ctx).Model("cluster_usage_batches").Data(gdb.Map{
		"node_id": nodeID, "batch_id": batchID, "rows": 0,
	}).Insert()
	if err == nil {
		return false, 0, nil
	}
	if !usageDuplicateError(err) {
		return false, 0, gerror.Wrap(err, "claim usage batch")
	}
	model := tx.Ctx(ctx).Model("cluster_usage_batches").
		Where("node_id", nodeID).Where("batch_id", batchID)
	if s.database.GetConfig().Type == "mysql" {
		// 当前读而非一致性快照读，看到唯一键等待者刚提交的 rows。
		model = model.LockUpdate()
	}
	record, err := model.Fields("rows").One()
	if err != nil {
		return false, 0, gerror.Wrap(err, "read committed usage batch")
	}
	if record.IsEmpty() {
		return false, 0, gerror.New("cluster: conflicting usage batch is missing")
	}
	return true, record["rows"].Int(), nil
}

// 普通 INSERT 只忽略明确的唯一键冲突。MySQL INSERT IGNORE 还会把非重复的
// 截断/非空等错误降级为 warning，不能用来判断账务写入是否成功。
func (s *Service) insertUsageRows(ctx context.Context, tx gdb.TX, nodeID uint64, rows []map[string]any) ([]map[string]any, error) {
	inserted := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := make(gdb.Map, len(row)+1)
		for key, value := range row {
			item[key] = value
		}
		item["node_id"] = nodeID
		result, err := tx.Ctx(ctx).Model("usage_logs").Data(item).Insert()
		if err != nil {
			if usageDuplicateError(err) {
				continue
			}
			return nil, gerror.Wrap(err, "insert usage row")
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, gerror.Wrap(err, "read inserted usage row count")
		}
		if affected != 1 {
			return nil, gerror.Newf("cluster: unexpected usage insert count %d", affected)
		}
		inserted = append(inserted, row)
	}
	return inserted, nil
}

func usageDuplicateError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	// SQLite 的扩展码：PRIMARY KEY / UNIQUE；不把所有 constraint 错误吞掉。
	var sqliteErr interface{ Code() int }
	return errors.As(err, &sqliteErr) && (sqliteErr.Code() == 1555 || sqliteErr.Code() == 2067)
}

// Billing 实现必须使用 txCtx 参与本事务；错误不能转为日志后继续 ACK。
func (s *Service) billUsageRows(ctx context.Context, rows []map[string]any) error {
	for _, row := range rows {
		cost, charged := usageCostOf(row)
		if !charged {
			continue
		}
		if s.billing == nil {
			return gerror.New("cluster: charged usage requires billing")
		}
		if cost.IsZero() {
			continue
		}
		if err := s.billing.Debit(ctx, uintValueOf(row["user_id"]), cost); err != nil {
			return gerror.Wrap(err, "debit usage user")
		}
		if err := s.billing.AddSpend(ctx, uintValueOf(row["api_key_id"]), cost.InexactFloat64()); err != nil {
			return gerror.Wrap(err, "add usage key spend")
		}
	}
	return nil
}

// usageCostOf 只承认显式布尔 charged=true；缺失、损坏或类型错误绝不收费。
func usageCostOf(row map[string]any) (decimal.Decimal, bool) {
	charged, err := usageCharged(row["billing_details_json"])
	if err != nil || !charged {
		return decimal.Zero, false
	}
	cost, err := usageAmount(row["estimated_cost"])
	if err != nil {
		return decimal.Zero, false
	}
	return cost, true
}

func uintValueOf(value any) uint64 {
	parsed, err := usageUint(value)
	if err != nil {
		return 0
	}
	return parsed
}
