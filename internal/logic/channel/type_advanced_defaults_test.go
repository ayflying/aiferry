package channel

import (
	"encoding/json"
	"testing"
)

// Gemini 的 OpenAI 兼容层拒收 prompt_cache_key（400 Unknown name），
// 因此该类型在渠道未显式配置时必须默认按 off 处置。
func TestParseChannelAdvancedConfigAppliesGeminiPromptCacheDefault(t *testing.T) {
	for _, raw := range []string{"", "null", "{}"} {
		config, err := ParseChannelAdvancedConfig([]byte(raw), GeminiChannelType)
		if err != nil {
			t.Fatalf("ParseChannelAdvancedConfig(%q) error = %v", raw, err)
		}
		if config.PromptCacheMode != PromptCacheModeOff {
			t.Fatalf("raw=%q promptCacheMode = %q, want off", raw, config.PromptCacheMode)
		}
		if actual := config.ResolvePromptCacheMode(); actual != PromptCacheModeOff {
			t.Fatalf("raw=%q resolved mode = %q, want off", raw, actual)
		}
	}
}

// 类型默认值必须可被渠道显式配置覆盖，否则等于把管理员设置变成死值。
func TestParseChannelAdvancedConfigKeepsExplicitPromptCacheMode(t *testing.T) {
	for _, mode := range []string{PromptCacheModeStable, PromptCacheModePassthrough, PromptCacheModeOff} {
		config, err := ParseChannelAdvancedConfig([]byte(`{"promptCacheMode":"`+mode+`"}`), GeminiChannelType)
		if err != nil {
			t.Fatalf("ParseChannelAdvancedConfig(%s) error = %v", mode, err)
		}
		if config.PromptCacheMode != mode {
			t.Fatalf("promptCacheMode = %q, want %q", config.PromptCacheMode, mode)
		}
	}
}

// 历史渠道用 passthroughPromptCache 表达「跟随客户端」，优先级高于 promptCacheMode，
// 类型默认值不得把它盖掉。
func TestParseChannelAdvancedConfigKeepsLegacyPassthroughForGemini(t *testing.T) {
	config, err := ParseChannelAdvancedConfig([]byte(`{"passthroughPromptCache":true}`), GeminiChannelType)
	if err != nil {
		t.Fatalf("ParseChannelAdvancedConfig() error = %v", err)
	}
	if actual := config.ResolvePromptCacheMode(); actual != PromptCacheModePassthrough {
		t.Fatalf("resolved mode = %q, want passthrough", actual)
	}
}

// 类型默认值只对声明的类型生效，其它渠道保持原有缺省行为（stable）。
func TestParseChannelAdvancedConfigLeavesOtherTypesUnchanged(t *testing.T) {
	for _, channelType := range []string{"openai", "sub2api", "ollama", ""} {
		config, err := ParseChannelAdvancedConfig(nil, channelType)
		if err != nil {
			t.Fatalf("ParseChannelAdvancedConfig(%q) error = %v", channelType, err)
		}
		if config.PromptCacheMode != "" {
			t.Fatalf("type=%q promptCacheMode = %q, want empty", channelType, config.PromptCacheMode)
		}
		if actual := config.ResolvePromptCacheMode(); actual != PromptCacheModeStable {
			t.Fatalf("type=%q resolved mode = %q, want stable", channelType, actual)
		}
	}
}

// 端到端约束：gemini 渠道经默认值后，客户端带来的缓存字段被剥离且不下发任何缓存键；
// 对照组 openai 仍然注入网关稳定键，保证默认值没有扩大化。
func TestGeminiChannelStripsPromptCacheFieldsEndToEnd(t *testing.T) {
	body := []byte(`{"model":"m","messages":[],"prompt_cache_key":"client-key","prompt_cache_options":{"mode":"implicit"}}`)
	identity := "v1|u:1|m:m|c:42|k:63"

	geminiConfig, err := ParseChannelAdvancedConfig(nil, GeminiChannelType)
	if err != nil {
		t.Fatalf("parse gemini config: %v", err)
	}
	geminiBody, err := ApplyPromptCachePolicy(body, geminiConfig, identity)
	if err != nil {
		t.Fatalf("apply gemini policy: %v", err)
	}
	var geminiPayload map[string]any
	if err = json.Unmarshal(geminiBody, &geminiPayload); err != nil {
		t.Fatalf("decode gemini body: %v", err)
	}
	for _, field := range promptCacheControlFields {
		if _, exists := geminiPayload[field]; exists {
			t.Fatalf("gemini body still contains %q", field)
		}
	}

	openAIConfig, err := ParseChannelAdvancedConfig(nil, "openai")
	if err != nil {
		t.Fatalf("parse openai config: %v", err)
	}
	openAIBody, err := ApplyPromptCachePolicy(body, openAIConfig, identity)
	if err != nil {
		t.Fatalf("apply openai policy: %v", err)
	}
	var openAIPayload map[string]any
	if err = json.Unmarshal(openAIBody, &openAIPayload); err != nil {
		t.Fatalf("decode openai body: %v", err)
	}
	if openAIPayload["prompt_cache_key"] != PromptCacheKey(identity) {
		t.Fatalf("openai prompt_cache_key = %v, want %q", openAIPayload["prompt_cache_key"], PromptCacheKey(identity))
	}
}
