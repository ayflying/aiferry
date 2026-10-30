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
	return s.addSpend(ctx, key.Id, key.KeyHash, amount)
}

// AddSpendByID 供集群记账使用：主节点收到子节点回传的用量时只知道密钥 ID。
// 缓存失效依赖 KeyHash，这里拿不到就跳过，由密钥缓存的 TTL 兜底。
func (s *sAPIKey) AddSpendByID(ctx context.Context, id uint64, amount float64) error {
	return s.addSpend(ctx, id, "", amount)
}

// addSpend 是累计密钥消耗的唯一实现，转发侧与集群回传侧共用，
// 避免两边各写一份跨天重置逻辑而产生偏差。
func (s *sAPIKey) addSpend(ctx context.Context, id uint64, keyHash string, amount float64) error {
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
			Where(columns.Id, id).
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
	if keyHash != "" {
		_ = s.app.Redis.Del(ctx, cacheKey(keyHash)).Err()
	}
	return nil
}
