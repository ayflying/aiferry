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
// 在同一记账事务内读取 KeyHash，确保主节点真实的鉴权缓存立即失效。
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
		// SQLite 使用延迟事务，先读后写在并发时无法升级读锁。先获取写锁，
		// 再读取累计值，避免 SQLITE_BUSY 和多个请求覆盖彼此的消耗。
		if dao.ApiKeys.DB().GetConfig().Type == "sqlite" {
			if _, err := dao.ApiKeys.Ctx(txCtx).Where(columns.Id, id).
				Data(do.ApiKeys{SpentAmount: gdb.Raw(columns.SpentAmount)}).Update(); err != nil {
				return gerror.Wrap(err, "lock SQLite API key spend")
			}
		}
		var stored entity.ApiKeys
		query := dao.ApiKeys.Ctx(txCtx).
			Fields(columns.Id, columns.KeyHash, columns.SpentAmount, columns.DailySpentAmount, columns.DailySpendDate).
			Where(columns.Id, id)
		// SQLite 事务负责串行化写入，不支持 SELECT ... FOR UPDATE。
		if dao.ApiKeys.DB().GetConfig().Type != "sqlite" {
			query = query.Lock(gdb.LockForUpdate)
		}
		if err := query.Scan(&stored); err != nil {
			return gerror.Wrap(err, "lock API key spend")
		}
		if stored.Id == 0 {
			return gerror.New("API key not found")
		}
		keyHash = stored.KeyHash
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
