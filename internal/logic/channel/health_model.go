package channel

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// selectHealthCheckModelID chooses a configured enabled model, or the first
// enabled model because callers load candidates in ascending ID order.
func selectHealthCheckModelID(configuredID uint64, models []entity.ChannelModels) uint64 {
	for _, model := range models {
		if configuredID == 0 || model.Id == configuredID {
			return model.Id
		}
	}
	return 0
}

// validateHealthCheckModel 校验保存渠道时提交的测试模型。用户可见错误一律中文：
// 模型已删除/停用时返回可操作的提示，不把 sql no rows 之类技术细节抛到弹框。
func (s *sChannel) validateHealthCheckModel(ctx context.Context, channelID, modelID uint64) (any, error) {
	if modelID == 0 {
		return gdb.Raw("NULL"), nil
	}
	var model entity.ChannelModels
	err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{
		Id:        modelID,
		ChannelId: channelID,
		Enabled:   1,
	}).Scan(&model)
	if lookupErr := healthCheckModelLookupError(err, model); lookupErr != nil {
		return nil, lookupErr
	}
	return model.Id, nil
}

// healthCheckModelLookupError 把测试模型查询结果翻译成用户可读的中文错误。
// no rows 与空主键都表示「配置的测试模型已不存在」，合并为同一句提示。
func healthCheckModelLookupError(scanErr error, model entity.ChannelModels) error {
	if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
		return gerror.Wrap(scanErr, "查询测试模型失败")
	}
	if scanErr != nil || model.Id == 0 {
		return gerror.New("测试模型不存在或已停用，请重新选择")
	}
	return nil
}

func channelAutoDisableEnabled(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
