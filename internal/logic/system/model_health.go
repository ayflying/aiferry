package system

import (
	"context"
	"fmt"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 模型健康评分常量：初始 100 分；真实转发成功 +1、模型测试成功 +5（上限 100）；
// 失败 -20，扣到 0 自动禁用该模型。账号级错误（余额/配额/组织停用）直接禁用渠道。
const (
	ModelHealthInitialScore   = 100
	ModelHealthMaxScore       = 100
	ModelHealthRelaySuccess   = 1
	ModelHealthTestSuccess    = 5
	ModelHealthFailurePenalty = 20
	ModelHealthDisableScore   = 0
)

type ModelDisableInput struct {
	ChannelID uint64
	ModelID   uint64
	ModelName string
	Source    string
	Status    int
	Message   string
	TimedOut  bool
}

// IsAccountLevelFailure 判断错误是否属于账号级问题。这类问题影响渠道内所有模型，
// 仍按原逻辑禁用整个渠道，而不是只扣单个模型的分。
func IsAccountLevelFailure(message string) bool {
	lower := strings.ToLower(message)
	accountKeywords := []string{
		"credit balance is too low",
		"insufficient account balance",
		"insufficient_quota",
		"exceeded your current quota",
		"organization has been disabled",
		"organization is not active",
		"account is not authorized",
		"permission denied",
		"operation not allowed",
		"security token included in the request is invalid",
		"daily usage limit exceeded",
		"usage limit exceeded",
		"billing",
		"arrears",
		"已欠费",
		"余额不足",
		"账户余额",
		"令牌无效",
		"令牌已过期",
		"无效的令牌",
		"令牌状态不可用",
		"该令牌无权使用模型",
		"访问凭证已过期",
		"无效的访问密钥",
		"access key has been disabled",
		"api key has been disabled",
		"invalid api key",
		"incorrect api key",
		"authentication Fails",
		"no auth credential",
		"check your plan and billing details",
		"deactivated",
		"deactivated_key",
		"user not found",
		"unauthorized",
	}
	for _, keyword := range accountKeywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func modelDisableReason(input ModelDisableInput) string {
	parts := make([]string, 0, 3)
	if input.Status > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", input.Status))
	}
	if input.TimedOut {
		parts = append(parts, "timed_out=true")
	}
	if message := strings.TrimSpace(input.Message); message != "" {
		parts = append(parts, message)
	}
	return truncate(strings.Join(parts, ", "), 1024)
}

// ApplyModelHealthScore 记录一次模型请求结果：成功加分、失败扣分。
// 返回是否触发了模型自动禁用。
func (s *sSystem) ApplyModelHealthScore(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input ModelDisableInput) (bool, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: input.ModelID}).Scan(&model); err != nil {
		return false, gerror.Wrap(err, "load channel model for health score")
	}
	if model.Id == 0 || model.Enabled != 1 {
		return false, nil
	}
	newScore := model.HealthScore
	if input.TimedOut || input.Status >= 400 || (input.Status == 0 && input.Message != "") {
		newScore -= ModelHealthFailurePenalty
	} else {
		newScore = min(newScore+ModelHealthRelaySuccess, ModelHealthMaxScore)
	}
	newScore = max(newScore, ModelHealthDisableScore)
	data := do.ChannelModels{HealthScore: newScore}
	disabled := false
	if newScore <= ModelHealthDisableScore && model.AutoDisabledAt == nil {
		disabled = true
		reason := modelDisableReason(input)
		source := autoDisableSource(input.Source)
		data.AutoDisabledAt = gtime.Now()
		data.AutoDisabledReason = reason
		data.AutoDisabledSource = source
	}
	if _, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: model.Id}).Data(data).Update(); err != nil {
		return false, gerror.Wrap(err, "update model health score")
	}
	if disabled {
		s.clearModelRouteCache(ctx)
		s.notifyAutoDisableTransition(ctx, settings, AutoDisableNotification{
			ChannelID:   input.ChannelID,
			ChannelName: s.autoDisableNotificationChannelName(ctx, input.ChannelID),
			Reason:      "模型 " + model.PublicName + " 健康评分降至 0：" + modelDisableReason(input),
			Source:      autoDisableSource(input.Source),
			StatusCode:  notificationStatusCode(input.Status),
			ModelName:   model.PublicName,
			ModelID:     model.Id,
		})
		s.scheduleChannelCloseIfAllModelsDown(ctx, input.ChannelID)
	}
	return disabled, nil
}

// ResetModelHealthScore 重置模型健康评分（手动恢复或测试成功恢复时使用）。
func (s *sSystem) ResetModelHealthScore(ctx context.Context, modelID uint64) error {
	_, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelID}).Data(do.ChannelModels{
		HealthScore:        ModelHealthInitialScore,
		AutoDisabledAt:     gdb.Raw("NULL"),
		AutoDisabledReason: gdb.Raw("NULL"),
		AutoDisabledSource: gdb.Raw("NULL"),
	}).Update()
	if err != nil {
		return gerror.Wrap(err, "reset model health score")
	}
	s.clearModelRouteCache(ctx)
	return nil
}

// RecoverModelIfAllowed 恢复被自动禁用的模型（清空禁用标记并重置评分）。
func (s *sSystem) RecoverModelIfAllowed(ctx context.Context, modelID uint64) (bool, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelID}).Scan(&model); err != nil {
		return false, gerror.Wrap(err, "load channel model for recovery")
	}
	if model.Id == 0 || model.AutoDisabledAt == nil {
		return false, nil
	}
	result, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: modelID, Enabled: 1}).Data(do.ChannelModels{
		HealthScore:        ModelHealthInitialScore,
		AutoDisabledAt:     gdb.Raw("NULL"),
		AutoDisabledReason: gdb.Raw("NULL"),
		AutoDisabledSource: gdb.Raw("NULL"),
	}).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically recover channel model")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearModelRouteCache(ctx)
	return true, nil
}

// scheduleChannelCloseIfAllModelsDown 渠道内已无可用启用模型时自动关闭渠道。
func (s *sSystem) scheduleChannelCloseIfAllModelsDown(ctx context.Context, channelID uint64) {
	var channel entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID}).Scan(&channel); err != nil || channel.Id == 0 {
		return
	}
	if channel.Status == 0 {
		return
	}
	modelColumns := dao.ChannelModels.Columns()
	count, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{
		ChannelId: channelID,
		Enabled:   1,
	}).WhereNull(modelColumns.AutoDisabledAt).Count()
	if err != nil {
		return
	}
	if count > 0 {
		return
	}
	reason := "渠道内所有启用模型均已被自动禁用"
	data := do.Channels{
		Status:                 0,
		AutoDisabledAt:         gtime.Now(),
		AutoDisabledReason:     reason,
		AutoDisabledSource:     AutoDisableSourceModelTest,
		AutoDisabledStatusCode: gdb.Raw("NULL"),
	}
	result, err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID, Status: 1}).Data(data).Update()
	if err != nil {
		return
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		s.clearTransient(ctx, channelID)
		settings, settingsErr := s.Get(ctx)
		if settingsErr == nil {
			s.notifyAutoDisableTransition(ctx, settings, AutoDisableNotification{
				ChannelID:   channel.Id,
				ChannelName: channel.Name,
				Reason:      reason,
				Source:      AutoDisableSourceModelTest,
			})
		}
	}
}

func (s *sSystem) clearModelRouteCache(ctx context.Context) {
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
	_ = s.app.Redis.Del(ctx, "aiferry:models:list").Err()
}
