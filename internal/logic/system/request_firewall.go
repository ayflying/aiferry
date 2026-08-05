package system

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	requestFirewallSettingsKey = "request_firewall"
	requestFirewallCacheKey    = "aiferry:system:request-firewall"
	requestFirewallCacheTTL    = 5 * time.Minute
)

func DefaultRequestFirewallSettings() adminapi.RequestFirewallSettingsInput {
	return adminapi.RequestFirewallSettingsInput{
		Enabled:                     true,
		MaxConcurrentRequests:       256,
		MaxConcurrentRequestsPerIP:  32,
		MaxConcurrentRequestsPerKey: 64,
		RequestsPerMinutePerIP:      600,
		RequestsPerMinutePerAPIKey:  600,
	}
}

func (s *sSystem) GetRequestFirewallSettings(ctx context.Context) (adminapi.RequestFirewallSettingsInput, error) {
	if cached, err := s.app.Redis.Get(ctx, requestFirewallCacheKey).Bytes(); err == nil {
		if settings, decodeErr := decodeRequestFirewallSettings(cached); decodeErr == nil {
			return settings, nil
		}
	}

	var row entity.SystemSettings
	if err := dao.SystemSettings.Ctx(ctx).Where(do.SystemSettings{SettingKey: requestFirewallSettingsKey}).Scan(&row); err != nil && !isNoRowsError(err) {
		return adminapi.RequestFirewallSettingsInput{}, gerror.Wrap(err, "load request firewall settings")
	}
	if row.SettingKey == "" {
		settings := DefaultRequestFirewallSettings()
		_ = s.cacheRequestFirewallSettings(ctx, settings)
		return settings, nil
	}
	settings, err := decodeRequestFirewallSettings([]byte(row.ValueJson))
	if err != nil {
		return adminapi.RequestFirewallSettingsInput{}, gerror.Wrap(err, "decode request firewall settings")
	}
	_ = s.cacheRequestFirewallSettings(ctx, settings)
	return settings, nil
}

func (s *sSystem) UpdateRequestFirewallSettings(ctx context.Context, input adminapi.RequestFirewallSettingsInput) (adminapi.RequestFirewallSettingsInput, error) {
	settings, err := normalizeRequestFirewallSettings(input)
	if err != nil {
		return adminapi.RequestFirewallSettingsInput{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return adminapi.RequestFirewallSettingsInput{}, gerror.Wrap(err, "encode request firewall settings")
	}
	result, err := dao.SystemSettings.Ctx(ctx).
		Where(do.SystemSettings{SettingKey: requestFirewallSettingsKey}).
		Data(do.SystemSettings{ValueJson: string(encoded)}).
		Update()
	if err != nil {
		return adminapi.RequestFirewallSettingsInput{}, gerror.Wrap(err, "update request firewall settings")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err = dao.SystemSettings.Ctx(ctx).Data(do.SystemSettings{SettingKey: requestFirewallSettingsKey, ValueJson: string(encoded)}).Insert(); err != nil {
			return adminapi.RequestFirewallSettingsInput{}, gerror.Wrap(err, "create request firewall settings")
		}
	}
	_ = s.app.Redis.Del(ctx, requestFirewallCacheKey).Err()
	_ = s.cacheRequestFirewallSettings(ctx, settings)
	return settings, nil
}

func (s *sSystem) cacheRequestFirewallSettings(ctx context.Context, settings adminapi.RequestFirewallSettingsInput) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.app.Redis.Set(ctx, requestFirewallCacheKey, encoded, requestFirewallCacheTTL).Err()
}

func decodeRequestFirewallSettings(value []byte) (adminapi.RequestFirewallSettingsInput, error) {
	settings := DefaultRequestFirewallSettings()
	if err := json.Unmarshal(value, &settings); err != nil {
		return adminapi.RequestFirewallSettingsInput{}, err
	}
	return normalizeRequestFirewallSettings(settings)
}

func normalizeRequestFirewallSettings(input adminapi.RequestFirewallSettingsInput) (adminapi.RequestFirewallSettingsInput, error) {
	if input.MaxConcurrentRequests < 1 || input.MaxConcurrentRequests > 4096 {
		return input, gerror.New("maxConcurrentRequests must be between 1 and 4096")
	}
	if input.MaxConcurrentRequestsPerIP < 1 || input.MaxConcurrentRequestsPerIP > 1024 {
		return input, gerror.New("maxConcurrentRequestsPerIp must be between 1 and 1024")
	}
	if input.MaxConcurrentRequestsPerKey < 1 || input.MaxConcurrentRequestsPerKey > 1024 {
		return input, gerror.New("maxConcurrentRequestsPerKey must be between 1 and 1024")
	}
	if input.MaxConcurrentRequestsPerIP > input.MaxConcurrentRequests || input.MaxConcurrentRequestsPerKey > input.MaxConcurrentRequests {
		return input, gerror.New("per-client concurrent limits must not exceed maxConcurrentRequests")
	}
	if input.RequestsPerMinutePerIP < 1 || input.RequestsPerMinutePerIP > 60000 {
		return input, gerror.New("requestsPerMinutePerIp must be between 1 and 60000")
	}
	if input.RequestsPerMinutePerAPIKey < 1 || input.RequestsPerMinutePerAPIKey > 60000 {
		return input, gerror.New("requestsPerMinutePerApiKey must be between 1 and 60000")
	}
	return input, nil
}
