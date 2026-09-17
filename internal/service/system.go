// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/system"
)

type (
	ISystem interface {
		DisableIfNeeded(ctx context.Context, input AutoDisableInput) (bool, error)
		DisableIfNeededWithSettings(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) (bool, error)
		RecoverIfAllowed(ctx context.Context, channelID uint64) (bool, error)
		RecoverCredentialIfAllowed(ctx context.Context, credentialID uint64) (bool, error)
		GetBase(ctx context.Context) (BaseSettings, error)
		UpdateBase(ctx context.Context, input adminapi.BaseSettingsInput) (BaseSettings, error)
		TimeZone(ctx context.Context) string
		// ApplyModelHealthScore 只更新本次实际使用的模型与凭证组合。
		ApplyModelHealthScore(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input ModelDisableInput) (bool, error)
		// BumpComboHealthScore 测试成功只恢复实际测试的组合，不触碰其他凭证。
		BumpComboHealthScore(ctx context.Context, modelID uint64, credentialID uint64, delta int) error
		// ResetModelHealthScore 是人工整模型重置，明确覆盖该模型的全部组合。
		ResetModelHealthScore(ctx context.Context, modelID uint64) error
		// RecoverModelIfAllowed 仅刷新旧模型标记与投影，不能重置全部组合。
		RecoverModelIfAllowed(ctx context.Context, modelID uint64) (bool, error)
		// DisplayCurrency 返回当前配置的展示货币，配置异常时回落 USD。
		DisplayCurrency(ctx context.Context) string
		// CurrencyRate 返回折算汇率，优先读 Redis 缓存。
		// 缓存未命中时按配置来源解析：auto 打公开接口，manual 直接用手填汇率。
		CurrencyRate(ctx context.Context) CurrencyRate
		// Convert 把金额按当前汇率折算到展示货币。
		// 币种缺失按 USD 处理；出现不支持的币种时原样返回，宁可不换算也不套错汇率。
		Convert(ctx context.Context, amount float64, from string) float64
		// ConvertTo 把金额从 from 折算到 to，两者都在支持币种内才会换算。
		ConvertTo(ctx context.Context, amount float64, from string, to string) float64
		GetSystemInformation(ctx context.Context) (adminapi.SystemInformationInput, error)
		UpdateSystemInformation(ctx context.Context, input adminapi.SystemInformationInput) (adminapi.SystemInformationInput, error)
		// ResolveSystemInformation fills the public server URL without changing the stored configuration.
		ResolveSystemInformation(ctx context.Context, fallbackServerURL string) (adminapi.SystemInformationInput, error)
		GetMailSettings(ctx context.Context) (MailSettings, error)
		UpdateMailSettings(ctx context.Context, input adminapi.MailSettingsInput) (MailSettings, error)
		MailDeliverySettings(ctx context.Context) (MailDeliverySettings, error)
		GetModelQualitySettings(ctx context.Context) (adminapi.ModelQualitySettingsInput, error)
		UpdateModelQualitySettings(ctx context.Context, input adminapi.ModelQualitySettingsInput) (adminapi.ModelQualitySettingsInput, error)
		RecordModelQualityEvent(ctx context.Context, input ModelQualityEventInput) error
		ListModelQualityEvents(ctx context.Context, input adminapi.ModelQualityEventsInput) (adminapi.ModelQualityEventList, error)
		ClearAutoDisableFailures(ctx context.Context, credentialID uint64)
		ClearChannelAutoDisableFailures(ctx context.Context, channelID uint64)
		ResetCredentialRecoverySchedule(ctx context.Context, credentialID uint64)
		ResetChannelRecoverySchedule(ctx context.Context, channelID uint64)
		BeginRecoveryAttempt(ctx context.Context, target RecoveryTarget, id uint64, autoDisabledAt time.Time) (bool, error)
		FinishRecoveryAttempt(ctx context.Context, target RecoveryTarget, id uint64, succeeded bool)
		GetRequestFirewallSettings(ctx context.Context) (adminapi.RequestFirewallSettingsInput, error)
		UpdateRequestFirewallSettings(ctx context.Context, input adminapi.RequestFirewallSettingsInput) (adminapi.RequestFirewallSettingsInput, error)
		GetSensitiveWordSettings(ctx context.Context) (adminapi.SensitiveWordSettingsInput, error)
		UpdateSensitiveWordSettings(ctx context.Context, input adminapi.SensitiveWordSettingsInput) (adminapi.SensitiveWordSettingsInput, error)
		CheckSensitivePrompt(ctx context.Context, endpoint string, body []byte) error
		Get(ctx context.Context) (adminapi.SystemResilienceSettingsInput, error)
		Update(ctx context.Context, input adminapi.SystemResilienceSettingsInput) (adminapi.SystemResilienceSettingsInput, error)
	}
)

var (
	localSystem ISystem
)

func System() ISystem {
	if localSystem == nil {
		panic("implement not found for interface ISystem, forgot register?")
	}
	return localSystem
}

func RegisterSystem(i ISystem) {
	localSystem = i
}
