package channel

import (
	"context"

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

func (s *sChannel) validateHealthCheckModel(ctx context.Context, channelID, modelID uint64) (any, error) {
	if modelID == 0 {
		return gdb.Raw("NULL"), nil
	}
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{
		Id:        modelID,
		ChannelId: channelID,
		Enabled:   1,
	}).Scan(&model); err != nil {
		return nil, gerror.Wrap(err, "find channel test model")
	}
	if model.Id == 0 {
		return nil, gerror.New("test model must be an enabled model of this channel")
	}
	return model.Id, nil
}

func channelAutoDisableEnabled(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
