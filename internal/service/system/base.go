package system

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	baseSettingsKey = "base_settings"
	baseCacheKey    = "aiferry:system:base-settings"
)

type BaseSettings struct {
	TimeZone string `json:"timeZone"`
}

func DefaultBaseSettings() BaseSettings {
	return BaseSettings{TimeZone: "Asia/Shanghai"}
}

func (s *Service) GetBase(ctx context.Context) (BaseSettings, error) {
	if cached, err := s.app.Redis.Get(ctx, baseCacheKey).Bytes(); err == nil {
		if settings, decodeErr := decodeBaseSettings(cached); decodeErr == nil {
			return settings, nil
		}
	}

	var row entity.SystemSettings
	if err := dao.SystemSettings.Ctx(ctx).Where(do.SystemSettings{SettingKey: baseSettingsKey}).Scan(&row); err != nil && !isNoRowsError(err) {
		return BaseSettings{}, gerror.Wrap(err, "load base settings")
	}
	if row.SettingKey == "" {
		return s.UpdateBase(ctx, adminapi.BaseSettingsInput{TimeZone: DefaultBaseSettings().TimeZone})
	}
	settings, err := decodeBaseSettings([]byte(row.ValueJson))
	if err != nil {
		return BaseSettings{}, gerror.Wrap(err, "decode base settings")
	}
	_ = s.cacheBase(ctx, settings)
	return settings, nil
}

func (s *Service) UpdateBase(ctx context.Context, input adminapi.BaseSettingsInput) (BaseSettings, error) {
	settings, err := normalizeBaseSettings(BaseSettings{TimeZone: input.TimeZone})
	if err != nil {
		return BaseSettings{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return BaseSettings{}, gerror.Wrap(err, "encode base settings")
	}
	result, err := dao.SystemSettings.Ctx(ctx).
		Where(do.SystemSettings{SettingKey: baseSettingsKey}).
		Data(do.SystemSettings{ValueJson: string(encoded)}).
		Update()
	if err != nil {
		return BaseSettings{}, gerror.Wrap(err, "update base settings")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err = dao.SystemSettings.Ctx(ctx).Data(do.SystemSettings{SettingKey: baseSettingsKey, ValueJson: string(encoded)}).Insert(); err != nil {
			return BaseSettings{}, gerror.Wrap(err, "create base settings")
		}
	}
	_ = s.app.Redis.Del(ctx, baseCacheKey).Err()
	_ = s.cacheBase(ctx, settings)
	return settings, nil
}

func (s *Service) TimeZone(ctx context.Context) string {
	settings, err := s.GetBase(ctx)
	if err != nil {
		return DefaultBaseSettings().TimeZone
	}
	return settings.TimeZone
}

func (s *Service) cacheBase(ctx context.Context, settings BaseSettings) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.app.Redis.Set(ctx, baseCacheKey, encoded, resilienceCacheTTL).Err()
}

func decodeBaseSettings(value []byte) (BaseSettings, error) {
	settings := DefaultBaseSettings()
	if err := json.Unmarshal(value, &settings); err != nil {
		return BaseSettings{}, err
	}
	return normalizeBaseSettings(settings)
}

func normalizeBaseSettings(settings BaseSettings) (BaseSettings, error) {
	location, err := time.LoadLocation(strings.TrimSpace(settings.TimeZone))
	if err != nil {
		return BaseSettings{}, gerror.Wrap(err, "timeZone is invalid")
	}
	settings.TimeZone = location.String()
	return settings, nil
}
