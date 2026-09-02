package channel

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// bumpModelHealthScore 给模型健康加分（封顶 100）。若模型因此从禁用态回升到
// 可用阈值以上，同时清除禁用标记。
func (s *sChannel) bumpModelHealthScore(ctx context.Context, modelID uint64, delta int) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Scan(&model); err != nil || model.Id == 0 {
		return
	}
	newScore := model.HealthScore + delta
	if newScore > system.ModelHealthMaxScore {
		newScore = system.ModelHealthMaxScore
	}
	if newScore < system.ModelHealthDisableScore {
		newScore = system.ModelHealthDisableScore
	}
	data := do.ChannelModels{HealthScore: newScore}
	if model.AutoDisabledAt != nil && newScore > system.ModelHealthDisableScore {
		data.AutoDisabledAt = gdb.Raw("NULL")
		data.AutoDisabledReason = gdb.Raw("NULL")
		data.AutoDisabledSource = gdb.Raw("NULL")
	}
	_, _ = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Data(data).Update()
}
