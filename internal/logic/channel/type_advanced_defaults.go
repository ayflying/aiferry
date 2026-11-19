package channel

// GeminiChannelType 是 Google Gemini 官方渠道类型编码，对应
// manifest/builtins.json 里的 built-in 类型 "gemini"。
const GeminiChannelType = "gemini"

// ApplyChannelTypeAdvancedDefaults 把渠道类型决定的默认高级配置套到渠道自身配置上。
//
// 适用边界：只有渠道「没有显式声明」的字段才采用类型默认值。渠道高级配置存在
// 数据库里，因此必须保留管理员手动覆盖的能力——类型默认值只是新建渠道的起点，
// 不是无法改写的行为。
//
// 为什么需要这一层：渠道高级配置的本质是「上游协议能力」，其中一部分不是可选项
// 而是硬约束（例如上游对未知字段做白名单校验，网关就不能注入任何缓存控制字段）。
// 把这类约束下沉到渠道类型，新建同类型渠道自动获得正确行为，无需每个渠道重复配置，
// 也避免「测试通过、正式流量 400」这类只在真实流量上暴露的问题。
func ApplyChannelTypeAdvancedDefaults(config AdvancedConfig, channelType string) AdvancedConfig {
	switch channelType {
	case GeminiChannelType:
		return applyGeminiAdvancedDefaults(config)
	default:
		return config
	}
}

// ParseChannelAdvancedConfig 解析渠道高级配置并套用渠道类型的默认值。
//
// 所有决定上游请求行为的链路（转发、协议回退、模型测试）都必须走本函数，
// 这样「类型默认值」只有一处解释，不会出现某条链路漏套默认值、
// 导致同配置在不同链路表现不一致。
//
// 只用于管理端展示（把渠道自身配置回显给表单）的调用点继续使用
// ParseAdvancedConfig：展示必须忠实反映数据库里的值，不能把类型默认值
// 混进渠道配置，否则管理员保存表单时会把默认值误写成渠道显式配置。
func ParseChannelAdvancedConfig(raw []byte, channelType string) (AdvancedConfig, error) {
	config, err := ParseAdvancedConfig(raw)
	if err != nil {
		return AdvancedConfig{}, err
	}
	return ApplyChannelTypeAdvancedDefaults(config, channelType), nil
}

// applyGeminiAdvancedDefaults 让 Gemini 渠道默认不下发提示缓存字段。
//
// Gemini 的 OpenAI 兼容层对请求体做严格的白名单校验，出现任何未知字段直接返回
// HTTP 400：
//
//	Invalid JSON payload received. Unknown name "prompt_cache_key": Cannot find field.
//
// 而网关缺省（stable）会主动注入 prompt_cache_key，因此 Gemini 渠道在未显式配置时
// 必须按 off 处置：只剥离客户端带来的缓存字段，一个都不下发。
//
// 管理员显式选择 stable 或 passthrough 时仍然尊重其选择，不做强制改写。
func applyGeminiAdvancedDefaults(config AdvancedConfig) AdvancedConfig {
	if config.PassthroughPromptCache || config.PromptCacheMode != "" {
		return config
	}
	config.PromptCacheMode = PromptCacheModeOff
	return config
}
