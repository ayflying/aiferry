package channel

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// healthCheckModelIDExpression keeps explicit health-check model choices first,
// and otherwise selects this channel's first enabled model deterministically.
func healthCheckModelIDExpression(channelAlias string) string {
	return fmt.Sprintf(
		"COALESCE(%[1]s.health_check_model_id,(SELECT fallback.id FROM channel_models fallback WHERE fallback.channel_id=%[1]s.id AND fallback.enabled=1 AND fallback.deleted_at IS NULL ORDER BY fallback.id ASC LIMIT 1))",
		channelAlias,
	)
}

func healthCheckModelJoin(channelAlias, modelAlias string) string {
	return fmt.Sprintf(
		"%[2]s.id=%[1]s AND %[2]s.channel_id=%[3]s.id AND %[2]s.enabled=1 AND %[2]s.deleted_at IS NULL",
		healthCheckModelIDExpression(channelAlias),
		modelAlias,
		channelAlias,
	)
}

func (s *Service) validateHealthCheckModel(ctx context.Context, channelID, modelID uint64) (any, error) {
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
