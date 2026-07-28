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
		GetSystemInformation(ctx context.Context) (adminapi.SystemInformationInput, error)
		UpdateSystemInformation(ctx context.Context, input adminapi.SystemInformationInput) (adminapi.SystemInformationInput, error)
		// ResolveSystemInformation fills the public server URL without changing the stored configuration.
		ResolveSystemInformation(ctx context.Context, fallbackServerURL string) (adminapi.SystemInformationInput, error)
		GetMailSettings(ctx context.Context) (MailSettings, error)
		UpdateMailSettings(ctx context.Context, input adminapi.MailSettingsInput) (MailSettings, error)
		MailDeliverySettings(ctx context.Context) (MailDeliverySettings, error)
		ClearAutoDisableFailures(ctx context.Context, credentialID uint64)
		ClearChannelAutoDisableFailures(ctx context.Context, channelID uint64)
		ResetCredentialRecoverySchedule(ctx context.Context, credentialID uint64)
		ResetChannelRecoverySchedule(ctx context.Context, channelID uint64)
		BeginRecoveryAttempt(ctx context.Context, target RecoveryTarget, id uint64, autoDisabledAt time.Time) (bool, error)
		FinishRecoveryAttempt(ctx context.Context, target RecoveryTarget, id uint64, succeeded bool)
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
