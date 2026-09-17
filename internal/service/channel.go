// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	"net/http"

	"github.com/shopspring/decimal"
	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type (
	IChannel interface {
		// AudioAdapterFor 返回渠道类型声明的音频适配器（openai / chat）。
		// 类型不存在或配置缺失时回退为 openai（标准 /audio/* 端点）。
		AudioAdapterFor(ctx context.Context, channelTypeCode string) (string, error)
		SetStatus(ctx context.Context, channelID uint64, status int) error
		List(ctx context.Context) ([]View, error)
		Get(ctx context.Context, id uint64) (entity.Channels, error)
		Create(ctx context.Context, input adminapi.ChannelInput) (uint64, error)
		Update(ctx context.Context, id uint64, input adminapi.ChannelInput) error
		Delete(ctx context.Context, id uint64) error
		QueryCost(ctx context.Context, channelID uint64, input adminapi.CostQueryInput) (CostResult, error)
		// RestoreCostQueryDisabledCredentials restores only credentials that were
		// previously disabled by the removed balance-query rule. Cost data is for
		// display and notification, not proof that a credential is unusable.
		RestoreCostQueryDisabledCredentials(ctx context.Context) error
		StartCostSync(ctx context.Context)
		// ApplyUsageCost preserves the legacy channel-level accounting entry point for
		// callers that do not know the selected credential. Relay requests use the
		// credential-aware method below.
		ApplyUsageCost(ctx context.Context, channelID uint64, amount decimal.Decimal) error
		ApplyCredentialUsageCost(ctx context.Context, channelID uint64, credentialID uint64, amount decimal.Decimal) error
		CreateCredential(ctx context.Context, channelID uint64, input adminapi.ChannelCredentialInput) (uint64, error)
		ListCredentials(ctx context.Context, channelID uint64) ([]CredentialView, error)
		// SetCredentialManagementKey 为单把凭证设置或清除管理密钥。传 nil 或空白
		// 表示清除（回到渠道级回退）。管理密钥与推理密钥是两个独立凭据：推理密钥
		// 用于请求转发，管理密钥用于该上游账号的用量/余额/额度查询。
		SetCredentialManagementKey(ctx context.Context, channelID uint64, credentialID uint64, input adminapi.ChannelCredentialManagementKeyInput) error
		SetCredentialStatus(ctx context.Context, channelID uint64, credentialID uint64, input adminapi.ChannelCredentialStatusInput) error
		DeleteCredential(ctx context.Context, channelID uint64, credentialID uint64) error
		HasAvailableCredential(ctx context.Context, channelID uint64) (bool, error)
		// CredentialSkipReason 返回渠道在选凭证阶段会被整体跳过的具体原因，
		// 供 relay 的 attempts==0 诊断日志与用量记录使用：
		//   - "N 把密钥全部冷却中"：启用密钥存在但都在 Redis 冷却（连续失败触发）；
		//   - "无启用密钥"：渠道没有 status=1 的密钥（被手动禁用或自动禁用）；
		//   - "有可用密钥"：不应出现——出现说明排除集合（excluded）把密钥全部排掉。
		CredentialSkipReason(ctx context.Context, channelID uint64) (string, error)
		// SelectCredential 为本请求挑选一把上游密钥。modelID 用于读取「模型 × 凭证」组合的健康分
		// 与冷却：处于组合冷却中的密钥被排除（冷却到期后可被真实流量探测恢复，不被 0 分永久排除）；
		// 组合分数越低被选中的概率越低（低分加权），但不直接归零，仍为恢复探测保留窗口。
		// 绑定亲和优先：若 apiKey 已绑定某券且它当前可选（未被排除、未冷却），直接复用，绑定亲和
		// 不得绕过冷却。
		SelectCredential(ctx context.Context, apiKeyID uint64, channelID uint64, modelID uint64, excluded map[uint64]struct{}) (RouteCredential, error)
		CredentialForTest(ctx context.Context, channelID uint64, credentialID uint64) (RouteCredential, error)
		StartHealthChecks(ctx context.Context)
		InvalidateListCache(ctx context.Context)
		DiscoverModels(ctx context.Context, channelID uint64) ([]DiscoveredModel, error)
		SelectModels(ctx context.Context, channelID uint64, input adminapi.ModelSelectionInput) ([]ModelView, error)
		ListModels(ctx context.Context, channelID uint64) ([]ModelView, error)
		DeleteFailedModels(ctx context.Context, channelID uint64) (int, error)
		ListPublicModels(ctx context.Context) ([]PublicModelView, error)
		UpdateModel(ctx context.Context, id uint64, input adminapi.ModelInput) error
		UpdatePublicModelPrice(ctx context.Context, id uint64, input adminapi.ModelPriceInput) error
		SyncAllPrices(ctx context.Context) (PriceSyncResult, error)
		SyncPriceSource(ctx context.Context, channelID uint64) (PriceSyncResult, error)
		SyncExternalPriceSource(ctx context.Context, sourceID uint64, sourceName string, baseURL string, config channeltype.PricingConfig) (PriceSyncResult, error)
		ListPriceRules(ctx context.Context, modelID uint64) ([]PriceRuleView, error)
		CreatePriceRule(ctx context.Context, modelID uint64, input adminapi.PriceRuleInput) (uint64, error)
		UpdatePriceRule(ctx context.Context, id uint64, input adminapi.PriceRuleInput) error
		DeletePriceRule(ctx context.Context, id uint64) error
		HTTPClientForProxy(proxyURLCipher string) (*http.Client, error)
		// QueryQuota 查询渠道上游的套餐额度（如智谱 GLM Coding Plan 的积分窗口）。
		// credentialID 为 0 时并发查询全部密钥并合并视图，结果按渠道缓存一分钟，
		// 重复点击不会重复请求上游，refresh 为 true 时绕过缓存强制查询；
		// credentialID 非零时只查询该密钥，不合并、不缓存（管理端即时诊断操作）。
		QueryQuota(ctx context.Context, channelID uint64, credentialID uint64, refresh bool) (QuotaView, error)
		TestModel(ctx context.Context, input adminapi.ModelTestInput, userID uint64) (TestResult, error)
		// UpstreamAuthSpecForChannelType 解析渠道类型声明的上游鉴权规则。
		// 内置类型走内存表，自定义类型走缓存，配置变更后转发与测试同时生效。
		UpstreamAuthSpecForChannelType(ctx context.Context, channelTypeCode string) (UpstreamAuthSpec, error)
		// ApplyUpstreamAuthHeaders 按渠道类型声明的鉴权规则，把鉴权头与基础头写入上游
		// 请求。正式转发（relay）与管理端模型测试、模型发现/同步共用这一实现：
		// 渠道类型的 authType / headerName / headerPrefix 只在这里解释一次，
		// 配置变更不会再出现"测试通过、正式转发 401"的两套行为。
		ApplyUpstreamAuthHeaders(req *http.Request, in UpstreamAuthInput) error
		// VideoAdapterFor 返回渠道类型声明的视频适配器（openai / minimax / volcengine_ark）。
		// 类型不存在或配置缺失时回退为 openai（标准 /videos 端点）。
		VideoAdapterFor(ctx context.Context, channelTypeCode string) (string, error)
	}
)

var (
	localChannel IChannel
)

func Channel() IChannel {
	if localChannel == nil {
		panic("implement not found for interface IChannel, forgot register?")
	}
	return localChannel
}

func RegisterChannel(i IChannel) {
	localChannel = i
}
