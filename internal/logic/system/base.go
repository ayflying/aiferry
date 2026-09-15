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
	// DisplayCurrency 决定把所有金额折算成哪种货币展示。
	// 只影响展示口径：库内的结算金额、账单快照与结算币种都不会被改写。
	DisplayCurrency string `json:"displayCurrency"`
	// ExchangeRateMode 决定汇率来源：auto 从公开汇率接口取并缓存，manual 只用人工填写的汇率。
	ExchangeRateMode string `json:"exchangeRateMode"`
	// ManualUsdToCnyRate 是人工填写的 USD→CNY 汇率，
	// 既是 manual 模式的取值，也是 auto 模式取不到汇率时的兜底值。
	ManualUsdToCnyRate float64 `json:"manualUsdToCnyRate"`
}

func DefaultBaseSettings() BaseSettings {
	return BaseSettings{
		TimeZone:           "Asia/Shanghai",
		DisplayCurrency:    CurrencyUSD,
		ExchangeRateMode:   CurrencyRateModeAuto,
		ManualUsdToCnyRate: DefaultManualUsdToCnyRate,
	}
}

func (s *sSystem) GetBase(ctx context.Context) (BaseSettings, error) {
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

func (s *sSystem) UpdateBase(ctx context.Context, input adminapi.BaseSettingsInput) (BaseSettings, error) {
	settings, err := normalizeBaseSettings(BaseSettings{
		TimeZone:           input.TimeZone,
		DisplayCurrency:    input.DisplayCurrency,
		ExchangeRateMode:   input.ExchangeRateMode,
		ManualUsdToCnyRate: input.ManualUsdToCnyRate,
	})
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

func (s *sSystem) TimeZone(ctx context.Context) string {
	settings, err := s.GetBase(ctx)
	if err != nil {
		return DefaultBaseSettings().TimeZone
	}
	return settings.TimeZone
}

func (s *sSystem) cacheBase(ctx context.Context, settings BaseSettings) error {
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
	// 货币相关字段一律「非法回落默认值」而不是报错：
	// 旧版本前端只提交 timeZone，空值必须能兼容，不能让整个基础设置保存失败。
	settings.DisplayCurrency = normalizeDisplayCurrency(settings.DisplayCurrency)
	settings.ExchangeRateMode = normalizeExchangeRateMode(settings.ExchangeRateMode)
	settings.ManualUsdToCnyRate = normalizeManualUsdToCnyRate(settings.ManualUsdToCnyRate)
	return settings, nil
}
