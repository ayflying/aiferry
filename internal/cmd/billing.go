package cmd

import (
	"context"

	"github.com/shopspring/decimal"
)

// balanceDebiter 是用户余额扣费能力的窄接口。
//
// 这里刻意用接口而不是具体类型：user/apikey 逻辑包的类型未导出，
// 而集群层只需要这两个方法。
type balanceDebiter interface {
	Debit(ctx context.Context, id uint64, amount decimal.Decimal) error
}

// keySpender 是访问密钥消耗累计能力的窄接口。
type keySpender interface {
	AddSpendByID(ctx context.Context, id uint64, amount float64) error
}

// clusterBilling 把转发侧使用的记账实现直接复用给集群层，
// 保证子节点回传的用量与主节点自身流量走完全相同的扣费语义。
type clusterBilling struct {
	users balanceDebiter
	keys  keySpender
}

func (b clusterBilling) Debit(ctx context.Context, userID uint64, amount decimal.Decimal) error {
	if b.users == nil {
		return nil
	}
	return b.users.Debit(ctx, userID, amount)
}

func (b clusterBilling) AddSpend(ctx context.Context, apiKeyID uint64, amount float64) error {
	if b.keys == nil {
		return nil
	}
	return b.keys.AddSpendByID(ctx, apiKeyID, amount)
}
