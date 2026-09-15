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
		var supplied struct {
			Endpoints json.RawMessage `json:"endpoints"`
			Audio     json.RawMessage `json:"audio"`
			Video     json.RawMessage `json:"video"`
		}
		if err := json.Unmarshal(value, &supplied); err != nil {
			return Config{}, gerror.Wrap(err, "invalid channel type JSON")
		}
		if supplied.Endpoints != nil {
			config.Endpoints = make(map[string]EndpointConfig)
		}
		if supplied.Audio != nil {
			config.Audio = AudioConfig{}
		}
		if supplied.Video != nil {
			config.Video = VideoConfig{}
		}
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
	if err := normalizeQuotaConfig(&config.Quota); err != nil {
		return Config{}, err
	}
	if err := normalizeEndpointConfigs(config.Endpoints); err != nil {
		return Config{}, err
	}
	config.Audio.Adapter = strings.ToLower(strings.TrimSpace(config.Audio.Adapter))
	switch config.Audio.Adapter {
	case "", AudioAdapterOpenAI:
		config.Audio.Adapter = AudioAdapterOpenAI
	case AudioAdapterChat:
	default:
		return Config{}, gerror.Newf("unsupported audio adapter %q (expected openai or chat)", config.Audio.Adapter)
	}
	config.Video.Adapter = strings.ToLower(strings.TrimSpace(config.Video.Adapter))
	switch config.Video.Adapter {
	case "", VideoAdapterOpenAI:
		config.Video.Adapter = VideoAdapterOpenAI
	case VideoAdapterMiniMax, VideoAdapterVolcengineArk:
	default:
		return Config{}, gerror.Newf("unsupported video adapter %q (expected openai, minimax or volcengine_ark)", config.Video.Adapter)
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

// IsAbsoluteHTTPURL 报告 value 是否是 http(s)://host 形式的完整地址。渠道类型
// 里「路径」类字段都接受完整地址：填完整地址时直接使用、不再拼接渠道根地址。
// 这是必需的——部分上游的查询接口不在根地址的版本前缀之下（根地址是
// https://host/v1，余额接口却是 https://host/api/user/self），甚至位于另一个
// 域名，靠拼接无法得到正确地址。
//
// 配置校验与运行期地址解析共用这一个判定，避免出现「能存不能查」的偏差。
func IsAbsoluteHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// validatePathField 校验「路径」类字段的形状：以 / 开头的相对路径，或完整
// HTTP(S) 地址。用于额度查询路径——它的相对路径按根地址的 host 根解析
// （而非拼在根地址之后），漏掉前导斜杠的写法无法判定意图，因此提前拦下。
// 费用/模型/价格路径的解析层能容忍无斜杠写法（拼接时会补斜杠），故不套用。
func validatePathField(field, value string) error {
	if strings.HasPrefix(value, "/") || IsAbsoluteHTTPURL(value) {
		return nil
	}
	return gerror.Newf("%s must start with / or be a full HTTP(S) URL", field)
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

// normalizeQuotaConfig 校验套餐额度查询配置。启用 adapter 时默认 GET 请求、
// 智谱用量接口路径与 channel_key 认证（额度接口总是使用渠道推理密钥）。
// volcengine_afp 例外：火山 GetAFPUsage 是控制面 V4 签名 POST 请求，path 固定、
// 鉴权走渠道管理密钥（AK/SK），method/path/header 字段被忽略。
func normalizeQuotaConfig(config *QuotaConfig) error {
	config.Adapter = strings.ToLower(strings.TrimSpace(config.Adapter))
	switch config.Adapter {
	case "", AdapterNone:
		config.Adapter = AdapterNone
		*config = QuotaConfig{Adapter: AdapterNone}
		return nil
	case AdapterZhipuQuota:
	case AdapterOpenCodeGo:
		// OpenCode Go /usage 接口位于 baseUrl 路径之下（…/zen/go/v1/usage），
		// 与智谱（host 根路径）不同，endpoint 拼接由 quota 解析层用完整 URL
		// 处理；path 直接使用完整 URL，不走下方以 / 开头的绝对路径校验。
		config.Method = httpMethodGet
		config.AuthType = AuthChannelKey
		config.HeaderName = "Authorization"
		config.HeaderPrefix = "Bearer "
		return nil
	case AdapterVolcAFP:
		// V4 签名接口：固定 POST + 管理密钥鉴权，path 仅用于校验留空兼容。
		config.Method = httpMethodGet
		config.AuthType = AuthManagementKey
		config.HeaderName = "Authorization"
		config.HeaderPrefix = "Bearer "
		if !strings.HasPrefix(config.Path, "/") {
			config.Path = "/"
		}
		return nil
	default:
		return gerror.Newf("unsupported quota adapter %q (expected none, %s, %s or %s)", config.Adapter, AdapterZhipuQuota, AdapterOpenCodeGo, AdapterVolcAFP)
	}
	config.Method = strings.ToUpper(strings.TrimSpace(config.Method))
	if config.Method == "" {
		config.Method = httpMethodGet
	}
	if config.Method != httpMethodGet {
		return gerror.New("quota adapter only supports the GET method")
	}
	config.Path = strings.TrimSpace(config.Path)
	if config.Path == "" {
		config.Path = "/api/monitor/usage/quota/limit"
	}
	// 额度接口不一定挂在渠道根地址之下：以 / 开头时按根地址的 host 根解析
	// （智谱 …/api/coding/paas/v4 + /api/monitor/…），填完整地址则直连。
	if err := validatePathField("quota.path", config.Path); err != nil {
		return err
	}
	config.AuthType = normalizeAuth(config.AuthType)
	if config.AuthType == AuthNone {
		config.AuthType = AuthChannelKey
	}
	config.HeaderName = normalizeHeader(config.HeaderName, config.AuthType)
	if !validAuth(config.AuthType) {
		return gerror.Newf("unsupported quota authType for %s", config.Adapter)
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
	config.ValueType = strings.TrimSpace(config.ValueType)
	if config.ValueType == "" {
		config.ValueType = ValueTypeCost
	}
	config.Method = normalizeMethod(config.Method)
	config.Path = strings.TrimSpace(config.Path)
	config.AuthType = normalizeAuth(config.AuthType)
	config.HeaderName = normalizeHeader(config.HeaderName, config.AuthType)
	config.FixedCurrency = strings.ToUpper(strings.TrimSpace(config.FixedCurrency))
	config.UsagePath = strings.TrimSpace(config.UsagePath)
	config.UsageUnit = strings.TrimSpace(config.UsageUnit)
	config.UsageType = strings.TrimSpace(config.UsageType)
	config.UsageDimension = strings.TrimSpace(config.UsageDimension)
	if !validCostAdapter(config.Adapter) {
		return gerror.New("unsupported costs.adapter")
	}
	if config.ValueType != ValueTypeCost && config.ValueType != ValueTypeUsage {
		return gerror.New("unsupported costs.valueType")
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
	if config.Adapter == AdapterCustomJSON && config.ValueType == ValueTypeCost && config.UsedPath == "" && config.RemainingPath == "" {
		return gerror.New("custom_json costs require usedPath or remainingPath")
	}
	if config.Adapter == AdapterCustomJSON && config.ValueType == ValueTypeUsage && config.UsagePath == "" && config.UsedPath == "" {
		return gerror.New("custom_json usage requires usagePath or usedPath")
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
	return value == AdapterNone || value == AdapterOpenAICosts || value == AdapterSub2API || value == AdapterNewAPI || value == AdapterCustomJSON || value == AdapterQiniuCosts || value == AdapterSiliconFlow || value == AdapterOpenRouter || value == AdapterQiniuUsage
}
