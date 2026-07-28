package channel

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

const maxSystemPromptLength = 16 << 10

// AdvancedConfig controls how a channel normalizes request and response payloads.
// All optional request fields are blocked until explicitly enabled.
type AdvancedConfig struct {
	ForceOpenAIFormat      bool   `json:"forceOpenAIFormat"`
	ReasoningToContent     bool   `json:"reasoningToContent"`
	PassthroughRequestBody bool   `json:"passthroughRequestBody"`
	SkipAsyncPollingDelay  bool   `json:"skipAsyncPollingDelay"`
	SystemPrompt           string `json:"systemPrompt"`
	AppendSystemPrompt     bool   `json:"appendSystemPrompt"`
	AllowServiceTier       bool   `json:"allowServiceTier"`
	BlockStore             bool   `json:"blockStore"`
	AllowSafetyIdentifier  bool   `json:"allowSafetyIdentifier"`
	AllowInclude           bool   `json:"allowInclude"`
	AllowInferenceGeo      bool   `json:"allowInferenceGeo"`
}

func DefaultAdvancedConfig() AdvancedConfig {
	return AdvancedConfig{BlockStore: true}
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
	// Protocol conversion is now always enabled, but retain compatibility with
	// saved channel JSON that still contains the retired switch.
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
	return config, nil
}

func MarshalAdvancedConfig(config AdvancedConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", gerror.Wrap(err, "encode channel advanced config")
	}
	return string(data), nil
}
