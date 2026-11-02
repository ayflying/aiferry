package cluster

import "context"

// WithWorkerLedger 将一个已完成请求的本地扣费与用量写入视作同一同步临界区。
// 上游网络请求不持锁；仅在结束时短暂等待，快照不得穿过扣费与出站明细之间。
// 主节点及未启用集群时不改变原路径。
func WithWorkerLedger(ctx context.Context, work func(context.Context) error) error {
	s := activeService.Load()
	if s == nil || !s.Enabled() || !s.IsWorker() {
		return work(ctx)
	}
	s.ledgerMu.Lock()
	defer s.ledgerMu.Unlock()
	return work(ctx)
}
