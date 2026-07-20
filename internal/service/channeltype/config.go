package channeltype

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

const httpMethodGet = "GET"

func ParseConfig(raw []byte) (Config, error) {
	config := DefaultConfig()
	if value := bytes.TrimSpace(raw); len(value) > 0 && string(value) != "null" {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return Config{}, gerror.Wrap(err, "invalid channel type JSON")
		}
	}
	if err := normalizeBaseURL(&config.BaseURL); err != nil {
		return Config{}, err
	}
	if err := normalizeModelConfig(&config.Models); err != nil {
		return Config{}, err
	}
	if err := normalizeCostConfig(&config.Costs); err != nil {
		return Config{}, err
	}
	if err := normalizeEndpointConfigs(config.Endpoints); err != nil {
		return Config{}, err
	}
	pricing, err := ParsePricingConfig(config.Pricing)
	if err != nil {
		return Config{}, err
	}
	if pricing.Adapter == AdapterNewAPIRatio {
		return Config{}, gerror.New("newapi_ratio pricing is only supported by public price sources")
	}
	config.Pricing = pricing
	return config, nil
}

func normalizeBaseURL(value *string) error {
	*value = strings.TrimRight(strings.TrimSpace(*value), "/")
	if *value == "" {
		return nil
	}
	parsed, err := url.Parse(*value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return gerror.New("baseUrl must be an absolute HTTP(S) URL")
	}
	return nil
}

func normalizeEndpointConfigs(configs map[string]EndpointConfig) error {
	if len(configs) == 0 {
		return gerror.New("endpoints must not be empty")
	}
	for name, config := range configs {
		config.Method = strings.ToUpper(strings.TrimSpace(config.Method))
		config.Path = strings.TrimSpace(config.Path)
		config.RequestBody = strings.ToLower(strings.TrimSpace(config.RequestBody))
		config.AuthType = normalizeAuth(config.AuthType)
		config.HeaderName = normalizeHeader(config.HeaderName, config.AuthType)
		if name == "" || config.Path == "" || !strings.HasPrefix(config.Path, "/") {
			return gerror.New("each endpoint requires a non-empty name and absolute path")
		}
		if config.Method != httpMethodGet && config.Method != "POST" && config.Method != "DELETE" {
			return gerror.Newf("unsupported endpoint method for %s", name)
		}
		if config.RequestBody != "json" && config.RequestBody != "multipart" && config.RequestBody != "none" {
			return gerror.Newf("unsupported endpoint requestBody for %s", name)
		}
		if !validAuth(config.AuthType) {
			return gerror.Newf("unsupported endpoint authType for %s", name)
		}
		configs[name] = config
	}
	return nil
}

func ParsePricingConfig(config PricingConfig) (PricingConfig, error) {
	config.Adapter = strings.TrimSpace(config.Adapter)
	if config.Adapter == "" {
		config.Adapter = AdapterNone
	}
	config.Method = normalizeMethod(config.Method)
	config.Path = strings.TrimSpace(config.Path)
	config.AuthType = normalizeAuth(config.AuthType)
	config.HeaderName = normalizeHeader(config.HeaderName, config.AuthType)
	config.ListPath = strings.TrimSpace(config.ListPath)
	config.ModelPath = strings.TrimSpace(config.ModelPath)
	if config.Adapter == AdapterNone {
		return config, nil
	}
	if config.Path == "" {
		return PricingConfig{}, gerror.New("pricing.path is required")
	}
	if config.Method != httpMethodGet {
		return PricingConfig{}, gerror.New("only GET price synchronization is supported")
	}
	if !validAuth(config.AuthType) {
		return PricingConfig{}, gerror.New("unsupported pricing.authType")
	}
	switch config.Adapter {
	case "json":
		if config.ModelPath == "" {
			return PricingConfig{}, gerror.New("pricing.modelPath is required for json pricing")
		}
		if config.RatesPath == "" && config.InputPricePath == "" && config.CachedInputPricePath == "" && config.CacheWritePricePath == "" && config.OutputPricePath == "" && config.ImageInputPricePath == "" && config.AudioInputPricePath == "" && config.AudioOutputPricePath == "" && config.RequestPricePath == "" {
			return PricingConfig{}, gerror.New("pricing requires ratesPath or a configured price path")
		}
	case AdapterNewAPIRatio:
	default:
		return PricingConfig{}, gerror.New("unsupported pricing.adapter")
	}
	return config, nil
}

func normalizeModelConfig(config *ModelConfig) error {
	config.Method = normalizeMethod(config.Method)
	config.Path = strings.TrimSpace(config.Path)
	config.ListPath = strings.TrimSpace(config.ListPath)
	config.IDPath = strings.TrimSpace(config.IDPath)
	config.AuthType = normalizeAuth(config.AuthType)
	config.HeaderName = normalizeHeader(config.HeaderName, config.AuthType)
	if config.Path == "" || config.IDPath == "" {
		return gerror.New("models.path and models.idPath are required")
	}
	if config.Method != httpMethodGet {
		return gerror.New("only GET model discovery is supported")
	}
	if !validAuth(config.AuthType) {
		return gerror.New("unsupported models.authType")
	}
	return nil
}

func normalizeCostConfig(config *CostConfig) error {
	config.Adapter = strings.TrimSpace(config.Adapter)
	if config.Adapter == "" {
		config.Adapter = AdapterNone
	}
	config.Method = normalizeMethod(config.Method)
	config.Path = strings.TrimSpace(config.Path)
	config.AuthType = normalizeAuth(config.AuthType)
	config.HeaderName = normalizeHeader(config.HeaderName, config.AuthType)
	config.FixedCurrency = strings.ToUpper(strings.TrimSpace(config.FixedCurrency))
	if !validCostAdapter(config.Adapter) {
		return gerror.New("unsupported costs.adapter")
	}
	if config.Adapter == AdapterNone {
		return nil
	}
	if config.Path == "" {
		return gerror.New("costs.path is required when cost querying is enabled")
	}
	if config.Method != httpMethodGet {
		return gerror.New("only GET cost queries are supported")
	}
	if !validAuth(config.AuthType) {
		return gerror.New("unsupported costs.authType")
	}
	if config.Adapter == AdapterCustomJSON && config.UsedPath == "" && config.RemainingPath == "" {
		return gerror.New("custom_json costs require usedPath or remainingPath")
	}
	return nil
}

func normalizeMethod(value string) string {
	if value = strings.ToUpper(strings.TrimSpace(value)); value == "" {
		return httpMethodGet
	}
	return value
}

func normalizeAuth(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return AuthNone
	}
	return value
}

func normalizeHeader(value, authType string) string {
	value = strings.TrimSpace(value)
	if value == "" && authType != AuthNone {
		return "Authorization"
	}
	return value
}

func validAuth(value string) bool {
	return value == AuthNone || value == AuthChannelKey || value == AuthManagementKey
}

func validCostAdapter(value string) bool {
	return value == AdapterNone || value == AdapterOpenAICosts || value == AdapterSub2API || value == AdapterCustomJSON
}
