package cluster

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// usageBatch 是子节点回传用量的载荷。
//
// Rows 直接使用实体到列映射的结果，避免为 30 多个列再维护一份结构体；
// 列名与表结构天然一致，主节点可以按原样写入。
type usageBatch struct {
	BatchID string           `json:"batchId"`
	Rows    []map[string]any `json:"rows"`
}

// UsageAck 是主节点的回执。Accepted 是本次真正入账的行数，
// 重放（同一批次被重复提交）时 Replayed 为 true，子节点按成功处理即可。
type UsageAck struct {
	Accepted int  `json:"accepted"`
	Replayed bool `json:"replayed"`
}

// usageAck 是包内使用的别名，避免包内外两套名字。
type usageAck = UsageAck

// usageRowReservedFields 是接收端负责生成的列：主键由主节点数据库分配，
// node_id 由主节点按签名身份填入，不能让子节点自己声明。
var usageRowReservedFields = []string{"id", "node_id"}

// newBatchID 生成回传批次号，用于主节点幂等去重。
func newBatchID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return hex.EncodeToString(buffer)
}

// usageRows 把本地用量明细转成可上行的行集合。
func usageRows(rows []entity.UsageLogs) []map[string]any {
	return entityRows(rows, usageRowReservedFields)
}
