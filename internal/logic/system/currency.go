package system

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	// currencyRateCacheKey 缓存折算汇率：TTL 内所有展示共用同一份，
	// 避免管理端每次刷新都去打外部汇率接口。
	currencyRateCacheKey = "aiferry:system:currency-rate"
	currencyRateCacheTTL = 6 * time.Hour
	// exchangeRateEndpoint 是 USD 基准的公开汇率接口，不需要密钥。
	exchangeRateEndpoint = "https://open.er-api.com/v6/latest/USD"
	exchangeRateTimeout  = 8 * time.Second

	// CurrencyUSD 与 CurrencyCNY 是支持的展示货币，也是汇率表唯一对外暴露的币种。
	CurrencyUSD = "USD"
	CurrencyCNY = "CNY"

	// CurrencyRateModeAuto 从公开接口取汇率；CurrencyRateModeManual 只用人工填写的汇率。
	CurrencyRateModeAuto   = "auto"
	CurrencyRateModeManual = "manual"

	// CurrencyRateSourceAuto 表示汇率来自公开接口；
	// CurrencyRateSourceManual 表示人工填写；CurrencyRateSourceFallback 表示接口不可用时的兜底。
	CurrencyRateSourceAuto     = "auto"
	CurrencyRateSourceManual   = "manual"
	CurrencyRateSourceFallback = "fallback"

	// DefaultManualUsdToCnyRate 是人工汇率与外部接口兜底的默认值（1 USD 约合多少 CNY）。
	// 只在用户没填、且自动接口不可用时生效，取整便于识别「这是兜底值不是实时汇率」。
	DefaultManualUsdToCnyRate = 7.2
)

// supportedCurrencies 是折算与前端展示支持的币种集合。
// 上游账户出现集合外的币种时不做换算，直接按原币种展示，避免拿错汇率糊弄。
var supportedCurrencies = []string{CurrencyUSD, CurrencyCNY}

// CurrencyRate 描述一次折算所用的汇率与来源，前端据此标注，排查时也能直接核对外部汇率是否被取到。
//
// Rates 的语义是「1 个 Base 能换多少该币种」，因此换算公式为：
//
//	目标金额 = 原金额 / Rates[原币种] * Rates[目标币种]
type CurrencyRate struct {
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	Source    string             `json:"source"`
	UpdatedAt string             `json:"updatedAt,omitempty"`
}

// DisplayCurrency 返回当前配置的展示货币，配置异常时回落 USD。
func (s *sSystem) DisplayCurrency(ctx context.Context) string {
	settings, err := s.GetBase(ctx)
	if err != nil {
		return DefaultBaseSettings().DisplayCurrency
	}
	return settings.DisplayCurrency
}

// CurrencyRate 返回折算汇率，优先读 Redis 缓存。
// 缓存未命中时按配置来源解析：auto 打公开接口，manual 直接用手填汇率。
func (s *sSystem) CurrencyRate(ctx context.Context) CurrencyRate {
	if cached, err := s.app.Redis.Get(ctx, currencyRateCacheKey).Bytes(); err == nil {
		if rate, decodeErr := decodeCurrencyRate(cached); decodeErr == nil {
			return rate
		}
	}
	rate := s.resolveCurrencyRate(ctx)
	if encoded, err := json.Marshal(rate); err == nil {
		_ = s.app.Redis.Set(ctx, currencyRateCacheKey, encoded, currencyRateCacheTTL).Err()
	}
	return rate
}

func (s *sSystem) resolveCurrencyRate(ctx context.Context) CurrencyRate {
	settings, err := s.GetBase(ctx)
	if err != nil {
		settings = DefaultBaseSettings()
	}
	manual := manualCurrencyRate(settings.ManualUsdToCnyRate)
	if settings.ExchangeRateMode != CurrencyRateModeAuto {
		manual.Source = CurrencyRateSourceManual
		return manual
	}
	fetched, fetchErr := s.fetchExchangeRates(ctx)
	if fetchErr != nil {
		// 外部接口不可用时用人工汇率兜底，并标明来源，避免把兜底值当成实时汇率。
		g.Log().Warningf(ctx, "fetch exchange rates: %v", fetchErr)
		manual.Source = CurrencyRateSourceFallback
		return manual
	}
	return fetched
}

func manualCurrencyRate(usdToCny float64) CurrencyRate {
	if !(usdToCny > 0) {
		usdToCny = DefaultManualUsdToCnyRate
	}
	return CurrencyRate{
		Base:   CurrencyUSD,
		Rates:  map[string]float64{CurrencyUSD: 1, CurrencyCNY: usdToCny},
		Source: CurrencyRateSourceManual,
	}
}

func (s *sSystem) fetchExchangeRates(ctx context.Context) (CurrencyRate, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, exchangeRateEndpoint, nil)
	if err != nil {
		return CurrencyRate{}, gerror.Wrap(err, "build exchange rate request")
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.app.HTTP.Do(request)
	if err != nil {
		return CurrencyRate{}, gerror.Wrap(err, "request exchange rate")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return CurrencyRate{}, gerror.Newf("exchange rate endpoint returned %d", response.StatusCode)
	}
	var payload struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
		Update string             `json:"time_last_update_utc"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return CurrencyRate{}, gerror.Wrap(err, "decode exchange rate")
	}
	if payload.Result != "" && payload.Result != "success" {
		return CurrencyRate{}, gerror.Newf("exchange rate endpoint result %q", payload.Result)
	}
	rates := map[string]float64{CurrencyUSD: 1}
	for _, code := range supportedCurrencies {
		if code == CurrencyUSD {
			continue
		}
		value, ok := payload.Rates[code]
		if !ok || !(value > 0) {
			return CurrencyRate{}, gerror.Newf("exchange rate for %s is missing", code)
		}
		rates[code] = value
	}
	return CurrencyRate{Base: CurrencyUSD, Rates: rates, Source: CurrencyRateSourceAuto, UpdatedAt: payload.Update}, nil
}

// Convert 把金额按当前汇率折算到展示货币。
// 币种缺失按 USD 处理；出现不支持的币种时原样返回，宁可不换算也不套错汇率。
func (s *sSystem) Convert(ctx context.Context, amount float64, from string) float64 {
	return s.ConvertTo(ctx, amount, from, s.DisplayCurrency(ctx))
}

// ConvertTo 把金额从 from 折算到 to，两者都在支持币种内才会换算。
func (s *sSystem) ConvertTo(ctx context.Context, amount float64, from, to string) float64 {
	return convertWithRates(amount, from, to, s.CurrencyRate(ctx).Rates)
}

// convertWithRates 是折算的纯函数部分：汇率表由调用方传入，便于单测直接覆盖边界。
func convertWithRates(amount float64, from, to string, rates map[string]float64) float64 {
	from = normalizeCurrencyCode(from)
	to = normalizeCurrencyCode(to)
	if from == to || !isSupportedCurrency(from) || !isSupportedCurrency(to) {
		return amount
	}
	fromRate, toRate := rates[from], rates[to]
	if !(fromRate > 0) || !(toRate > 0) {
		return amount
	}
	return amount / fromRate * toRate
}

func normalizeCurrencyCode(value string) string {
	code := strings.ToUpper(strings.TrimSpace(value))
	if code == "" {
		return CurrencyUSD
	}
	return code
}

func isSupportedCurrency(code string) bool {
	for _, item := range supportedCurrencies {
		if item == code {
			return true
		}
	}
	return false
}

func decodeCurrencyRate(value []byte) (CurrencyRate, error) {
	var rate CurrencyRate
	if err := json.Unmarshal(value, &rate); err != nil {
		return CurrencyRate{}, err
	}
	if rate.Base == "" || len(rate.Rates) == 0 {
		return CurrencyRate{}, gerror.New("cached currency rate is incomplete")
	}
	return rate, nil
}

// normalizeDisplayCurrency 校验展示货币，空值与非法值回落 USD。
func normalizeDisplayCurrency(value string) string {
	code := normalizeCurrencyCode(value)
	if !isSupportedCurrency(code) {
		return CurrencyUSD
	}
	return code
}

// normalizeExchangeRateMode 校验汇率来源，空值与非法值回落 auto。
func normalizeExchangeRateMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CurrencyRateModeManual:
		return CurrencyRateModeManual
	case CurrencyRateModeAuto, "":
		return CurrencyRateModeAuto
	default:
		return CurrencyRateModeAuto
	}
}

// normalizeManualUsdToCnyRate 校验人工汇率：必须为正且不超过一个量级上界，非法时回落默认值。
func normalizeManualUsdToCnyRate(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 100 {
		return DefaultManualUsdToCnyRate
	}
	return value
}
