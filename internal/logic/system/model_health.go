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

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 模型健康评分常量：初始 100 分；模型测试成功 +5（上限 100）；
// 真实转发成功按响应耗时分级加分，越快加分越多；失败 -20，扣到 0 自动隔离该组合。
// 响应已写字节后的失败同样计分但力度更轻（-10），且绝不做渠道/密钥级禁用。
// 健康分同时作为路由加权系数：低于 80 分按比例降权，让持续出错的渠道逐步让出流量。
// 明确账号级错误仅禁用实际凭证。
//
// 评分单元：以 (channel_model_id, channel_credential_id) 组合为准，每个组合独立持有
// 健康分、冷却时间与最近错误，落到 channel_model_credentials 表。一个组合的失败只影响
// 它自己，不波及同模型的其它密钥、也不波及同密钥的其它模型。channel_models.health_score
// 仅作为该模型下「可用组合最高分」的投影，供路由加权使用。
const (
	ModelHealthInitialScore   = 100
	ModelHealthMaxScore       = 100
	ModelHealthTestSuccess    = 5
	ModelHealthFailurePenalty = 20
	// ModelHealthRateLimitPenalty 是限流（429）的扣分。限流与账号级/内容级失败性质不同，
	// 不应按常规失败 -20 的力度重罚，否则正常流量高峰会被快速打到隔离。
	ModelHealthRateLimitPenalty = 10
	// ModelHealthCommittedFailurePenalty 是「响应已向客户端写出内容后失败」的扣分。
	// 这类失败无法切换候选重放，但它同样是真实的上游质量信号；用比常规失败更温和
	// 的力度，避免零散断流把渠道直接推到禁用。
	ModelHealthCommittedFailurePenalty = 10
	// ModelHealthWeightThreshold 是健康分参与路由加权排序的门槛：分数低于该值才降权。
	// 正常渠道长期维持满分，行为不受影响；质量下滑的渠道按分数比例逐步让出流量，
	// 不必等到归零被禁用才生效。
	ModelHealthWeightThreshold = 80
	ModelHealthDisableScore    = 0
	// ComboCooldownMinutes 是组合健康分归零后的隔离时长：冷却期间该组合不参与选凭证，
	// 冷却到期后可被真实流量试探，避免被 0 分永久排除。
	ComboCooldownMinutes = 5
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
	// Committed 表示本次失败发生在响应已向客户端写出内容之后。它只影响扣分力度，
	// 不会把失败转移到渠道/密钥级禁用。
	Committed bool
}

// modelHealthFailurePenalty 按失败性质选择扣分力度：已写字节后的失败更温和。
func modelHealthFailurePenalty(input ModelDisableInput) int {
	if input.Committed {
		return ModelHealthCommittedFailurePenalty
	}
	return ModelHealthFailurePenalty
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
// 由凭证级禁用策略处理，不将单把凭证错误扩大到整个渠道。
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
		// 账号明确欠费 / 套餐到期的精准关键词：只命中「账号或套餐本身已失效」的措辞，
		// 不纳入宽泛的「续订」「expired」等易与正常响应（如"订阅即将到期提醒"）误匹配的词。
		"套餐已到期",
		"套餐已过期",
		"账户已欠费",
		"已欠费停机",
		"账号已欠费",
		"账户余额不足",
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

// shouldSkipDisabledModelRetry 已被自动禁用的模型收到模型测试来源的失败时，
// 不重复扣分或重写禁用标记：分数已在禁用时归零，恢复巡检负责用成功的测试
// 解禁。没有这层防护，恢复测试失败会在 0 分上反复归零、模型永不解禁——
// 生产曾出现恢复巡检连测 190 次、每次都撞限流又每次都归零的死循环。
func shouldSkipDisabledModelRetry(source string, autoDisabledAt *time.Time) bool {
	return source == AutoDisableSourceModelTest && autoDisabledAt != nil
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

// invalidateRouteWeights 只递增路由版本号：健康分参与候选加权排序，分数下降后
// 必须让下一次请求重新解析候选。与 clearModelRouteCache 的区别是不清理模型列表
// 缓存——那属于模型可用性变化，与权重调整无关。
func (s *sSystem) invalidateRouteWeights(ctx context.Context) {
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}

func (s *sSystem) clearModelRouteCache(ctx context.Context) {
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
	_ = s.app.Redis.Del(ctx, "aiferry:models:list").Err()
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
