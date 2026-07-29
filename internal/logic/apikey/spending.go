package apikey

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sAPIKey) AddSpend(ctx context.Context, key AuthKey, amount float64) error {
	if amount <= 0 {
		return nil
	}
	now := time.Now()
	today := now.Format(time.DateOnly)
	if err := dao.ApiKeys.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		columns := dao.ApiKeys.Columns()
		var stored entity.ApiKeys
		if err := dao.ApiKeys.Ctx(txCtx).
			Fields(columns.Id, columns.SpentAmount, columns.DailySpentAmount, columns.DailySpendDate).
			Where(columns.Id, key.Id).
			Lock(gdb.LockForUpdate).
			Scan(&stored); err != nil {
			return gerror.Wrap(err, "lock API key spend")
		}
		if stored.Id == 0 {
			return gerror.New("API key not found")
		}
		dailySpent := amount
		if stored.DailySpendDate.Format(time.DateOnly) == today {
			dailySpent += stored.DailySpentAmount
		}
		if _, err := dao.ApiKeys.Ctx(txCtx).Where(columns.Id, stored.Id).Data(do.ApiKeys{
			SpentAmount:      stored.SpentAmount + amount,
			DailySpentAmount: dailySpent,
			DailySpendDate:   today,
		}).Update(); err != nil {
			return gerror.Wrap(err, "add API key spend")
		}
		return nil
	}); err != nil {
		return err
	}
	_ = s.app.Redis.Del(ctx, cacheKey(key.KeyHash)).Err()
	return nil
}
