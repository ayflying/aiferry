package apikey

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *Service) AddSpend(ctx context.Context, key AuthKey, amount float64) error {
	if amount <= 0 {
		return nil
	}
	literal := decimalLiteral(amount)
	today := time.Now().Format(time.DateOnly)
	result, err := dao.ApiKeys.Ctx(ctx).Where(dao.ApiKeys.Columns().Id, key.Id).Data(do.ApiKeys{
		SpentAmount:      gdb.Raw("spent_amount + " + literal),
		DailySpentAmount: gdb.Raw("CASE WHEN daily_spend_date = '" + today + "' THEN daily_spent_amount + " + literal + " ELSE " + literal + " END"),
		DailySpendDate:   today,
	}).Update()
	if err != nil {
		return gerror.Wrap(err, "add API key spend")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("API key not found")
	}
	_ = s.app.Redis.Del(ctx, cacheKey(key.KeyHash)).Err()
	return nil
}
