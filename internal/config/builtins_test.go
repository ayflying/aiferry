package config

import (
	"encoding/json"
	"path/filepath"
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
		"jiapi": 9000000000000011, "newapi": 9000000000000012, "qiniu": 9000000000000013, "siliconflow": 9000000000000014,
		"opencode_go": 9000000000000015, "openrouter": 9000000000000016, "zhipu": 9000000000000017,
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
}
