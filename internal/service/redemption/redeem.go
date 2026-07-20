package redemption

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

func (s *Service) Redeem(ctx context.Context, userID uint64, input adminapi.RedemptionCodeRedeemInput) (RedeemResult, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if code == "" {
		return RedeemResult{}, gerror.New("请填写兑换码")
	}
	var result RedeemResult
	err := dao.RedemptionCodes.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		var row codeRow
		if err := dao.RedemptionCodes.Ctx(txCtx).Where(dao.RedemptionCodes.Columns().Code, code).Scan(&row); err != nil {
			return gerror.Wrap(err, "find redemption code")
		}
		if row.Id == 0 {
			return gerror.New("兑换码不存在")
		}
		now := time.Now()
		switch codeStatus(row, now) {
		case statusUsed:
			return gerror.New("兑换码已被使用")
		case statusExpired:
			return gerror.New("兑换码已过期")
		}
		amount, err := decimal.NewFromString(row.Amount)
		if err != nil {
			return gerror.Wrap(err, "parse redemption code amount")
		}
		updated, err := dao.RedemptionCodes.Ctx(txCtx).
			Where(dao.RedemptionCodes.Columns().Id, row.Id).
			WhereNull(dao.RedemptionCodes.Columns().RedeemedAt).
			Where("(`expires_at` IS NULL OR `expires_at` > ?)", now).
			Data(do.RedemptionCodes{RedeemedByUserId: userID, RedeemedAt: now}).
			Update()
		if err != nil {
			return gerror.Wrap(err, "redeem code")
		}
		if affected, _ := updated.RowsAffected(); affected == 0 {
			return gerror.New("兑换码已被使用或已过期")
		}
		if err = s.users.Credit(txCtx, userID, amount); err != nil {
			return err
		}
		result = RedeemResult{Code: row.Code, Amount: decimalFloat(amount)}
		return nil
	})
	return result, err
}
