package system

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 模型健康评分常量：初始 100 分；模型测试成功 +5（上限 100）；
// 真实转发成功按响应耗时分级加分，越快加分越多；失败 -20，扣到 0 自动禁用该模型。
// 账号级错误（余额/配额/组织停用）直接禁用渠道。
const (
	ModelHealthInitialScore   = 100
	ModelHealthMaxScore       = 100
	ModelHealthTestSuccess    = 5
	ModelHealthFailurePenalty = 20
	ModelHealthDisableScore   = 0
)

// ModelHealthRelaySuccessByLatency 返回真实转发成功后的健康分增量。
// 以端到端上游响应耗时为依据：更快的模型恢复/积累信誉更快，慢模型仍可恢复但更慢。
func ModelHealthRelaySuccessByLatency(latency time.Duration) int {
	switch {
	case latency <= time.Second:
		return 5
	case latency <= 3*time.Second:
		return 4
	case latency <= 10*time.Second:
		return 3
	case latency <= 30*time.Second:
		return 2
	default:
		return 1
	}
}

type ModelDisableInput struct {
	ChannelID           uint64
	ChannelCredentialID uint64
	ModelID             uint64
	ModelName           string
	Source              string
	Status              int
	Message             string
	TimedOut            bool
	Latency             time.Duration
}

// isCredentialScopedFailure 判断失败是否可归因于单个密钥/账号级问题（余额耗尽、配额用尽、鉴权失败）。
// 这类失败换一把密钥即可恢复，不应拖垮模型健康分。HTTP 402 几乎总是"密钥没费用"，
// 但上游文案千差万别，单独按状态码兜底。
func isCredentialScopedFailure(input ModelDisableInput) bool {
	if input.Status == http.StatusPaymentRequired {
		return true
	}
	return IsAccountLevelFailure(input.Message)
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
		"payment required",
		"insufficient balance",
		"insufficient credit",
		"quota exceeded",
		"quota exhausted",
		"exceeded your quota",
		"已欠费",
		"欠费",
		"余额不足",
		"余额耗尽",
		"账户余额",
		"额度不足",
		"额度已用尽",
		"额度用尽",
		"无可用额度",
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
		newScore = min(newScore+ModelHealthRelaySuccessByLatency(input.Latency), ModelHealthMaxScore)
	}
	newScore = max(newScore, ModelHealthDisableScore)
	data := do.ChannelModels{HealthScore: newScore}
	disabled := false
	if newScore <= ModelHealthDisableScore && model.AutoDisabledAt == nil {
		// 密钥级失败（欠费、配额用尽、鉴权失败）扣分扣到 0 时，若渠道排除当前失败密钥
		// 后仍有其他可用密钥，说明模型本身没问题：改为立即禁用失败密钥并把模型分数
		// 重置回初始值，而不是禁用模型导致整个渠道的模型被逐个拖垮。
		if input.ChannelCredentialID > 0 && isCredentialScopedFailure(input) {
			diverted, err := s.divertModelDisableToCredential(ctx, settings, model, input)
			if err != nil {
				return false, err
			}
			if diverted {
				return true, nil
			}
		}
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

// divertModelDisableToCredential 将模型禁用转移为密钥禁用：渠道排除当前失败密钥后
// 仍存在可用密钥时，立即禁用失败密钥并把模型健康分重置回初始值。
// 返回 true 表示已完成转移，调用方不再禁用模型。
func (s *sSystem) divertModelDisableToCredential(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, model entity.ChannelModels, input ModelDisableInput) (bool, error) {
	hasSpare, err := s.hasAvailableCredentialExcept(ctx, input.ChannelID, input.ChannelCredentialID)
	if err != nil {
		return false, err
	}
	if !hasSpare {
		return false, nil
	}
	var channel entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: input.ChannelID}).Scan(&channel); err != nil {
		return false, gerror.Wrap(err, "load channel for credential divert")
	}
	if channel.Id == 0 {
		return false, nil
	}
	var credential entity.ChannelCredentials
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: input.ChannelCredentialID}).Scan(&credential); err != nil {
		return false, gerror.Wrap(err, "load channel credential for credential divert")
	}
	if credential.Id == 0 {
		return false, nil
	}
	if credential.Status == 1 {
		// 扣分到 0 本身已代表该密钥连续多次失败，直接禁用，不再走阈值计数。
		if _, err := s.disableCredentialNow(ctx, channel, settings, credential, AutoDisableInput{
			ChannelID:           input.ChannelID,
			ChannelCredentialID: input.ChannelCredentialID,
			ChannelModelID:      input.ModelID,
			Source:              input.Source,
			Status:              input.Status,
			Latency:             input.Latency,
			Message:             input.Message,
			TimedOut:            input.TimedOut,
		}); err != nil {
			return false, err
		}
	}
	if _, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{Id: model.Id}).Data(do.ChannelModels{
		HealthScore:        ModelHealthInitialScore,
		AutoDisabledAt:     gdb.Raw("NULL"),
		AutoDisabledReason: gdb.Raw("NULL"),
		AutoDisabledSource: gdb.Raw("NULL"),
	}).Update(); err != nil {
		return false, gerror.Wrap(err, "reset model health score after credential divert")
	}
	s.clearModelRouteCache(ctx)
	return true, nil
}

// hasAvailableCredentialExcept 判断渠道是否存在指定密钥之外的其他可用密钥（启用且不在冷却中）。
func (s *sSystem) hasAvailableCredentialExcept(ctx context.Context, channelID, credentialID uint64) (bool, error) {
	rows := make([]entity.ChannelCredentials, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).
		Where(do.ChannelCredentials{ChannelId: channelID, Status: 1}).
		Where(dao.ChannelCredentials.Columns().Id+" <> ?", credentialID).
		Scan(&rows); err != nil {
		return false, gerror.Wrap(err, "list spare channel credentials")
	}
	for _, row := range rows {
		if cooling, _ := s.app.Redis.Exists(ctx, CredentialCooldownKey(row.Id)).Result(); cooling > 0 {
			continue
		}
		return true, nil
	}
	return false, nil
}
