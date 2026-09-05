package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBuiltins(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.ChannelTypes) != 17 {
		t.Fatalf("unexpected built-in registry: %+v", registry)
	}
	for code, id := range map[string]uint64{
		"openai": 9000000000000001, "anthropic": 9000000000000008,
		"aws_bedrock": 9000000000000009, "gemini": 9000000000000010,
		"newapi": 9000000000000012, "qiniu": 9000000000000013, "siliconflow": 9000000000000014,
		"opencode_go": 9000000000000015, "openrouter": 9000000000000016, "zhipu": 9000000000000017,
		"zhipu_api": 9000000000000018,
	} {
		if item, exists := registry.ChannelTypeByCode(code); !exists || item.ID != id {
			t.Fatalf("built-in channel type is missing: %s %+v", code, item)
		}
	}
}

func TestZhipuBuiltinDefaultsToCodingPlan(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	item, exists := registry.ChannelTypeByCode("zhipu")
	if !exists {
		t.Fatal("Zhipu channel type is missing")
	}
	var config struct {
		BaseURL   string                     `json:"baseUrl"`
		Quota     json.RawMessage            `json:"quota"`
		Endpoints map[string]json.RawMessage `json:"endpoints"`
	}
	if err = json.Unmarshal(item.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Fatalf("Zhipu base URL = %q", config.BaseURL)
	}
	if len(config.Endpoints) != 1 || config.Endpoints["chatCompletions"] == nil {
		t.Fatalf("Zhipu Coding Plan endpoints = %+v", config.Endpoints)
	}
	if !strings.Contains(string(config.Quota), `"zhipu_coding_plan"`) {
		t.Fatalf("Zhipu Coding Plan quota config = %s", config.Quota)
	}
}

func TestZhipuAPIBuiltinTargetsStandardEndpoint(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	item, exists := registry.ChannelTypeByCode("zhipu_api")
	if !exists {
		t.Fatal("Zhipu API channel type is missing")
	}
	var config struct {
		BaseURL string          `json:"baseUrl"`
		Quota   json.RawMessage `json:"quota"`
	}
	if err = json.Unmarshal(item.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://open.bigmodel.cn/api/paas/v4" {
		t.Fatalf("Zhipu API base URL = %q", config.BaseURL)
	}
	if len(config.Quota) != 0 {
		t.Fatalf("Zhipu API must not declare quota config, got %s", config.Quota)
	}
}
