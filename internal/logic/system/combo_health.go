package system

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"
	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// ApplyModelHealthScore 只更新本次实际使用的模型与凭证组合。
func (s *sSystem) ApplyModelHealthScore(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input ModelDisableInput) (bool, error) {
	if input.ChannelCredentialID == 0 || input.ModelID == 0 {
		return false, nil
	}
	success := input.Status >= 200 && input.Status < 300 && input.Message == "" && !input.TimedOut && !input.Committed
	delta := -modelHealthFailurePenalty(input)
	if input.Status == http.StatusTooManyRequests && !input.Committed {
		delta = -ModelHealthRateLimitPenalty
	}
	if success {
		delta = ModelHealthRelaySuccessByLatency(input.Latency)
	}
	err := s.changeComboHealth(ctx, input.ChannelID, input.ModelID, input.ChannelCredentialID, delta, success)
	return false, err
}

// changeComboHealth 统一先锁模型行，使初始化、组合增减与投影在同一事务串行完成。
func (s *sSystem) changeComboHealth(ctx context.Context, channelID, modelID, credentialID uint64, delta int, success bool) error {
	if credentialID == 0 {
		return nil
	}
	err := dao.ChannelModels.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		var model entity.ChannelModels
		if err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelID}).LockUpdate().Scan(&model); err != nil {
			return err
		}
		if model.Id == 0 || model.Enabled != 1 {
			return nil
		}
		if channelID != 0 && model.ChannelId != channelID {
			return gerror.New("组合模型不属于指定渠道")
		}
		var credential entity.ChannelCredentials
		if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID, ChannelId: model.ChannelId}).Scan(&credential); err != nil {
			return err
		}
		if credential.Id == 0 {
			return gerror.New("组合凭证不属于模型渠道")
		}
		var combo entity.ChannelModelCredentials
		where := do.ChannelModelCredentials{ChannelModelId: modelID, ChannelCredentialId: credentialID}
		if err := dao.ChannelModelCredentials.Ctx(ctx).Where(where).LockUpdate().Scan(&combo); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if combo.Id == 0 {
			id, err := dao.ChannelModelCredentials.Ctx(ctx).Data(do.ChannelModelCredentials{ChannelId: model.ChannelId, ChannelModelId: modelID, ChannelCredentialId: credentialID, HealthScore: 100}).InsertAndGetId()
			if err != nil {
				return err
			}
			combo.Id, combo.HealthScore = uint64(id), 100
		}
		score := max(0, min(100, combo.HealthScore+delta))
		data := do.ChannelModelCredentials{HealthScore: score}
		if score == 0 {
			data.CooldownUntil = gtime.Now().Add(5 * time.Minute)
		}
		// 不保存上游原始报错，避免密钥或请求内容经错误摘要泄露。
		if success {
			data.LastError = gdb.Raw("NULL")
		} else {
			data.LastError = "上游请求失败"
		}
		if _, err := dao.ChannelModelCredentials.Ctx(ctx).Where(do.ChannelModelCredentials{Id: combo.Id}).Data(data).Update(); err != nil {
			return err
		}
		if success && score > 0 {
			if _, err := dao.ChannelModelCredentials.Ctx(ctx).Where(do.ChannelModelCredentials{Id: combo.Id}).Data(dao.ChannelModelCredentials.Columns().CooldownUntil, gdb.Raw("NULL")).Update(); err != nil {
				return err
			}
		}
		return s.updateModelHealthProjection(ctx, model)
	})
	if err == nil && s.app != nil && s.app.Redis != nil {
		s.invalidateRouteWeights(ctx)
	}
	return err
}

// updateModelHealthProjection 将所有有效凭证纳入投影；尚无组合行的凭证默认满分。
func (s *sSystem) updateModelHealthProjection(ctx context.Context, model entity.ChannelModels) error {
	var credentials []entity.ChannelCredentials
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: model.ChannelId, Status: 1}).Scan(&credentials); err != nil {
		return err
	}
	var combos []entity.ChannelModelCredentials
	if err := dao.ChannelModelCredentials.Ctx(ctx).Where(do.ChannelModelCredentials{ChannelModelId: model.Id}).Scan(&combos); err != nil {
		return err
	}
	indexed := make(map[uint64]entity.ChannelModelCredentials, len(combos))
	for _, combo := range combos {
		indexed[combo.ChannelCredentialId] = combo
	}
	best := 0
	now := gtime.Now()
	for _, credential := range credentials {
		combo, exists := indexed[credential.Id]
		if !exists {
			best = 100
			continue
		}
		if combo.CooldownUntil != nil && combo.CooldownUntil.After(now) {
			continue
		}
		best = max(best, combo.HealthScore)
	}
	// 组合隔离绝不设置模型级禁用，否则冷却到期仍会被旧路由过滤。
	_, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: model.Id}).Data(do.ChannelModels{HealthScore: best, AutoDisabledAt: gdb.Raw("NULL"), AutoDisabledReason: gdb.Raw("NULL"), AutoDisabledSource: gdb.Raw("NULL")}).Update()
	return err
}

// BumpComboHealthScore 测试成功只恢复实际测试的组合，不触碰其他凭证。
func (s *sSystem) BumpComboHealthScore(ctx context.Context, modelID, credentialID uint64, delta int) error {
	return s.changeComboHealth(ctx, 0, modelID, credentialID, max(0, delta), true)
}

// ResetModelHealthScore 是人工整模型重置，明确覆盖该模型的全部组合。
func (s *sSystem) ResetModelHealthScore(ctx context.Context, modelID uint64) error {
	err := dao.ChannelModels.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		var model entity.ChannelModels
		if err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelID}).LockUpdate().Scan(&model); err != nil {
			return err
		}
		if model.Id == 0 {
			return nil
		}
		if _, err := dao.ChannelModelCredentials.Ctx(ctx).Where(do.ChannelModelCredentials{ChannelModelId: modelID}).Data(do.ChannelModelCredentials{HealthScore: 100, LastError: gdb.Raw("NULL")}).Data(dao.ChannelModelCredentials.Columns().CooldownUntil, gdb.Raw("NULL")).Update(); err != nil {
			return err
		}
		return s.updateModelHealthProjection(ctx, model)
	})
	if err == nil && s.app != nil && s.app.Redis != nil {
		s.clearModelRouteCache(ctx)
	}
	return err
}

// RecoverModelIfAllowed 仅刷新旧模型标记与投影，不能重置全部组合。
func (s *sSystem) RecoverModelIfAllowed(ctx context.Context, modelID uint64) (bool, error) {
	recovered := false
	err := dao.ChannelModels.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		var model entity.ChannelModels
		if err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelID}).LockUpdate().Scan(&model); err != nil {
			return err
		}
		if model.Id == 0 || model.Enabled != 1 || model.AutoDisabledAt == nil {
			return nil
		}
		recovered = true
		return s.updateModelHealthProjection(ctx, model)
	})
	if err == nil && recovered && s.app != nil && s.app.Redis != nil {
		s.clearModelRouteCache(ctx)
	}
	return recovered, err
}
