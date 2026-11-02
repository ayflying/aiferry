package user

import (
	"context"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sUser) Debit(ctx context.Context, id uint64, amount decimal.Decimal) error {
	return s.debit(ctx, id, amount, false)
}

// SettleUsage 结算子节点已经发生的消费。余额不足也必须记为欠款，不能因
// 同步延迟拒绝扣费后将已提供的服务永久漏账；普通实时扣费仍保持不透支语义。
func (s *sUser) SettleUsage(ctx context.Context, id uint64, amount decimal.Decimal) error {
	return s.debit(ctx, id, amount, true)
}

func (s *sUser) debit(ctx context.Context, id uint64, amount decimal.Decimal, allowDebt bool) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	amount = amount.Round(8)
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	return dao.Users.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		columns := dao.Users.Columns()
		var account entity.Users
		if s.app.Config.IsWorker() {
			// SQLite延迟事务在读取前先获取写锁，避免并发读后升级产生SQLITE_BUSY。
			if _, err := dao.Users.Ctx(txCtx).Where(columns.Id, id).
				Data(do.Users{Balance: gdb.Raw("balance")}).Update(); err != nil {
				return gerror.Wrap(err, "lock worker balance for debit")
			}
		}
		model := dao.Users.Ctx(txCtx).
			Fields(columns.Id, columns.Balance).
			Where(columns.Id, id)
		// SQLite由写事务串行化，不支持SELECT FOR UPDATE语法。
		if !s.app.Config.IsWorker() {
			model = model.Lock(gdb.LockForUpdate)
		}
		if err := model.Scan(&account); err != nil {
			return gerror.Wrap(err, "lock user balance for debit")
		}
		balance := decimal.NewFromFloat(account.Balance).Round(8)
		if account.Id == 0 {
			return gerror.New("user not found for debit")
		}
		if !allowDebt && balance.LessThan(amount) {
			return ErrInsufficientBalance
		}
		updatedBalance, _ := balance.Sub(amount).Round(8).Float64()
		if _, err := dao.Users.Ctx(txCtx).
			Where(columns.Id, account.Id).
			Data(do.Users{Balance: updatedBalance}).
			Update(); err != nil {
			return gerror.Wrap(err, "debit user balance")
		}
		return nil
	})
}
