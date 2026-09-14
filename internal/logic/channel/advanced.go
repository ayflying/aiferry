package channel

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

const maxSystemPromptLength = 16 << 10

// maxConcurrencyLimit 是单个上游密钥允许配置的并发上限。限制额度按密钥独立计数，
// 因此渠道的总并发是「密钥数 × 该值」，这里只约束单把密钥的上限。
const maxConcurrencyLimit = 1024

// AdvancedConfig controls how a channel normalizes request and response payloads.
// All optional request fields are blocked until explicitly enabled.
type AdvancedConfig struct {
	BackupBaseURLs         []string `json:"backupBaseUrls"`
	ForceOpenAIFormat      bool     `json:"forceOpenAIFormat"`
	ReasoningToContent     bool     `json:"reasoningToContent"`
	PassthroughRequestBody bool     `json:"passthroughRequestBody"`
	PassthroughPromptCache bool     `json:"passthroughPromptCache"`
	SkipAsyncPollingDelay  bool     `json:"skipAsyncPollingDelay"`
	SystemPrompt           string   `json:"systemPrompt"`
	AppendSystemPrompt     bool     `json:"appendSystemPrompt"`
	AllowServiceTier       bool     `json:"allowServiceTier"`
	BlockStore             bool     `json:"blockStore"`
	AllowSafetyIdentifier  bool     `json:"allowSafetyIdentifier"`
	AllowInclude           bool     `json:"allowInclude"`
	AllowInferenceGeo      bool     `json:"allowInferenceGeo"`
	// ConcurrencyLimit 是每把上游密钥允许同时进行的转发请求数，0 表示不限制。
	// 额度按「渠道 × 密钥」独立计数，同一渠道的多把密钥互不占用：
	// 配置 15 且渠道有 3 把密钥时，每把各 15 并发，渠道合计 45 并发。
	ConcurrencyLimit int `json:"concurrencyLimit"`
	// ProtocolConversion 控制该渠道是否参与 Chat Completions 与 Responses 的
	// 自动协议转换。nil 表示跟随系统设置（默认启用）；true 强制启用，
	// false 强制关闭：关闭后请求直连客户端声明的端点，也不会再回退到转换。
	ProtocolConversion *bool `json:"protocolConversion"`
}

func DefaultAdvancedConfig() AdvancedConfig {
	return AdvancedConfig{BackupBaseURLs: []string{}, BlockStore: true}
}

func ParseAdvancedConfig(raw []byte) (AdvancedConfig, error) {
	config := DefaultAdvancedConfig()
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return config, nil
	}
	fields := make(map[string]json.RawMessage)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil {
		return AdvancedConfig{}, gerror.Wrap(err, "decode channel advanced config")
	}
	// 已废弃的 enableProtocolConversion（曾被改成始终启用）仍需删除：历史渠道
	// JSON 里可能残留该字段，留着会触发下方的 DisallowUnknownFields 报错。
	// 现行开关是 ProtocolConversion，缺省表示跟随系统设置。
	delete(fields, "enableProtocolConversion")
	normalized, err := json.Marshal(fields)
	if err != nil {
		return AdvancedConfig{}, gerror.Wrap(err, "normalize channel advanced config")
	}
	decoder = json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return AdvancedConfig{}, gerror.Wrap(err, "decode channel advanced config")
	}
	config.SystemPrompt = strings.TrimSpace(config.SystemPrompt)
	if len(config.SystemPrompt) > maxSystemPromptLength {
		return AdvancedConfig{}, gerror.New("system prompt exceeds 16 KiB")
	}
	if config.ConcurrencyLimit < 0 || config.ConcurrencyLimit > maxConcurrencyLimit {
		return AdvancedConfig{}, gerror.New("concurrency limit must be between 0 and 1024")
	}
	return config, nil
}

func MarshalAdvancedConfig(config AdvancedConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", gerror.Wrap(err, "encode channel advanced config")
	}
	return string(data), nil
}

func (config AdvancedConfig) UpstreamBaseURLs(primary string) []string {
	urls := make([]string, 0, len(config.BackupBaseURLs)+1)
	seen := make(map[string]struct{}, len(config.BackupBaseURLs)+1)
	for _, value := range append([]string{primary}, config.BackupBaseURLs...) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	return urls
}
