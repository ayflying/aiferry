package system

import (
	"math"
	"testing"
)

func TestCurrencyDefaults(t *testing.T) {
	settings := DefaultBaseSettings()
	if settings.DisplayCurrency != CurrencyUSD {
		t.Fatalf("DisplayCurrency = %q, want %q", settings.DisplayCurrency, CurrencyUSD)
	}
	if settings.ExchangeRateMode != CurrencyRateModeAuto {
		t.Fatalf("ExchangeRateMode = %q, want %q", settings.ExchangeRateMode, CurrencyRateModeAuto)
	}
	if settings.ManualUsdToCnyRate != DefaultManualUsdToCnyRate {
		t.Fatalf("ManualUsdToCnyRate = %v, want %v", settings.ManualUsdToCnyRate, DefaultManualUsdToCnyRate)
	}
}

// 旧版本前端只提交 timeZone，货币字段会缺省送达。
// 这些字段必须回落默认值而不是报错，否则历史页面保存基础设置会直接失败。
func TestNormalizeBaseSettingsFillsCurrencyDefaults(t *testing.T) {
	settings, err := normalizeBaseSettings(BaseSettings{TimeZone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("normalizeBaseSettings() error = %v", err)
	}
	if settings.DisplayCurrency != CurrencyUSD {
		t.Fatalf("DisplayCurrency = %q, want %q", settings.DisplayCurrency, CurrencyUSD)
	}
	if settings.ExchangeRateMode != CurrencyRateModeAuto {
		t.Fatalf("ExchangeRateMode = %q, want %q", settings.ExchangeRateMode, CurrencyRateModeAuto)
	}
	if settings.ManualUsdToCnyRate != DefaultManualUsdToCnyRate {
		t.Fatalf("ManualUsdToCnyRate = %v, want fallback %v", settings.ManualUsdToCnyRate, DefaultManualUsdToCnyRate)
	}
}

func TestNormalizeBaseSettingsKeepsValidCurrency(t *testing.T) {
	settings, err := normalizeBaseSettings(BaseSettings{
		TimeZone:           "Asia/Shanghai",
		DisplayCurrency:    "cny",
		ExchangeRateMode:   "MANUAL",
		ManualUsdToCnyRate: 7.35,
	})
	if err != nil {
		t.Fatalf("normalizeBaseSettings() error = %v", err)
	}
	if settings.DisplayCurrency != CurrencyCNY {
		t.Fatalf("DisplayCurrency = %q, want %q", settings.DisplayCurrency, CurrencyCNY)
	}
	if settings.ExchangeRateMode != CurrencyRateModeManual {
		t.Fatalf("ExchangeRateMode = %q, want %q", settings.ExchangeRateMode, CurrencyRateModeManual)
	}
	if settings.ManualUsdToCnyRate != 7.35 {
		t.Fatalf("ManualUsdToCnyRate = %v, want 7.35", settings.ManualUsdToCnyRate)
	}
}

func TestNormalizeDisplayCurrency(t *testing.T) {
	cases := map[string]string{
		"":      CurrencyUSD,
		"USD":   CurrencyUSD,
		"usd":   CurrencyUSD,
		"CNY":   CurrencyCNY,
		" cny ": CurrencyCNY,
		"JPY":   CurrencyUSD,
		"usdt":  CurrencyUSD,
	}
	for input, want := range cases {
		if got := normalizeDisplayCurrency(input); got != want {
			t.Fatalf("normalizeDisplayCurrency(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeExchangeRateMode(t *testing.T) {
	cases := map[string]string{
		"":       CurrencyRateModeAuto,
		"auto":   CurrencyRateModeAuto,
		"AUTO":   CurrencyRateModeAuto,
		"manual": CurrencyRateModeManual,
		"Manual": CurrencyRateModeManual,
		"随机":     CurrencyRateModeAuto,
	}
	for input, want := range cases {
		if got := normalizeExchangeRateMode(input); got != want {
			t.Fatalf("normalizeExchangeRateMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeManualUsdToCnyRate(t *testing.T) {
	invalid := []float64{0, -1, -0.01, 100.5, 1e6, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, input := range invalid {
		if got := normalizeManualUsdToCnyRate(input); got != DefaultManualUsdToCnyRate {
			t.Fatalf("normalizeManualUsdToCnyRate(%v) = %v, want fallback %v", input, got, DefaultManualUsdToCnyRate)
		}
	}
	if got := normalizeManualUsdToCnyRate(7.35); got != 7.35 {
		t.Fatalf("normalizeManualUsdToCnyRate(7.35) = %v, want 7.35", got)
	}
}

func TestManualCurrencyRate(t *testing.T) {
	rate := manualCurrencyRate(7.35)
	if rate.Base != CurrencyUSD {
		t.Fatalf("Base = %q, want %q", rate.Base, CurrencyUSD)
	}
	if rate.Source != CurrencyRateSourceManual {
		t.Fatalf("Source = %q, want %q", rate.Source, CurrencyRateSourceManual)
	}
	if rate.Rates[CurrencyUSD] != 1 {
		t.Fatalf("USD rate = %v, want 1", rate.Rates[CurrencyUSD])
	}
	if rate.Rates[CurrencyCNY] != 7.35 {
		t.Fatalf("CNY rate = %v, want 7.35", rate.Rates[CurrencyCNY])
	}
	// 非法人工汇率必须回落到默认值，不能把 0 汇率带进折算。
	if fallback := manualCurrencyRate(0); fallback.Rates[CurrencyCNY] != DefaultManualUsdToCnyRate {
		t.Fatalf("fallback CNY rate = %v, want %v", fallback.Rates[CurrencyCNY], DefaultManualUsdToCnyRate)
	}
}

func TestConvertWithRates(t *testing.T) {
	rates := map[string]float64{CurrencyUSD: 1, CurrencyCNY: 7.2}
	// 7.2 元按 1 USD = 7.2 CNY 折算应当是 1 美元。
	if got := convertWithRates(7.2, CurrencyCNY, CurrencyUSD, rates); math.Abs(got-1) > 1e-9 {
		t.Fatalf("CNY->USD = %v, want 1", got)
	}
	// 反向折算回人民币应当还原成原值。
	if got := convertWithRates(1, CurrencyUSD, CurrencyCNY, rates); math.Abs(got-7.2) > 1e-9 {
		t.Fatalf("USD->CNY = %v, want 7.2", got)
	}
	// 汇率缺失时原样返回，宁可不折算也不套错汇率。
	if got := convertWithRates(100, "JPY", CurrencyUSD, rates); got != 100 {
		t.Fatalf("missing rate must return the original amount, got %v", got)
	}
	if got := convertWithRates(100, CurrencyCNY, CurrencyUSD, map[string]float64{CurrencyUSD: 1}); got != 100 {
		t.Fatalf("missing source rate must return the original amount, got %v", got)
	}
}

func TestNormalizeCurrencyCode(t *testing.T) {
	cases := map[string]string{
		"":      CurrencyUSD,
		"  ":    CurrencyUSD,
		"cny":   CurrencyCNY,
		" usd ": CurrencyUSD,
	}
	for input, want := range cases {
		if got := normalizeCurrencyCode(input); got != want {
			t.Fatalf("normalizeCurrencyCode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsSupportedCurrency(t *testing.T) {
	if !isSupportedCurrency(CurrencyUSD) || !isSupportedCurrency(CurrencyCNY) {
		t.Fatal("USD and CNY must be supported")
	}
	if isSupportedCurrency("JPY") || isSupportedCurrency("") {
		t.Fatal("unsupported currencies must be rejected")
	}
}

func TestDecodeCurrencyRate(t *testing.T) {
	rate, err := decodeCurrencyRate([]byte(`{"base":"USD","rates":{"USD":1,"CNY":7.2},"source":"auto"}`))
	if err != nil {
		t.Fatalf("decodeCurrencyRate() error = %v", err)
	}
	if rate.Rates[CurrencyCNY] != 7.2 {
		t.Fatalf("CNY rate = %v, want 7.2", rate.Rates[CurrencyCNY])
	}
	if _, err = decodeCurrencyRate([]byte(`not json`)); err == nil {
		t.Fatal("invalid cached payload must be rejected")
	}
	// 缓存缺 base 或空汇率表都视为不完整，必须回落到重新解析而不是拿半份汇率折算。
	if _, err = decodeCurrencyRate([]byte(`{"rates":{"CNY":7.2}}`)); err == nil {
		t.Fatal("cached payload without base must be rejected")
	}
	if _, err = decodeCurrencyRate([]byte(`{"base":"USD","rates":{}}`)); err == nil {
		t.Fatal("cached payload without rates must be rejected")
	}
}
