package usage

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/dao"
)

// CheckPayloadAccess 在读取磁盘前按日志验证调用归属。
func (s *sUsage) CheckPayloadAccess(ctx context.Context, requestID string, userID uint64, admin bool) error {
	return checkPayloadAccess(requestID, userID, admin, func(id string, ownerID uint64) (bool, error) {
		columns := dao.UsageLogs.Columns()
		query := dao.UsageLogs.Ctx(ctx).Where(columns.RequestId, id)
		if ownerID != 0 {
			query = query.Where(columns.UserId, ownerID)
		}
		count, err := query.Count()
		return count > 0, err
	})
}

// lookup 的 ownerID=0 仅用于已认证管理员；其他用户必须带本人过滤条件。
func checkPayloadAccess(requestID string, userID uint64, admin bool, lookup func(string, uint64) (bool, error)) error {
	requestID = strings.TrimSpace(requestID)
	if userID == 0 || requestID == "" {
		return gerror.New("报文不存在或无权查看")
	}
	ownerID := userID
	if admin {
		ownerID = 0
	}
	exists, err := lookup(requestID, ownerID)
	if err != nil {
		return gerror.Wrap(err, "核验报文访问权限")
	}
	if !exists {
		return gerror.New("报文不存在或无权查看")
	}
	return nil
}
