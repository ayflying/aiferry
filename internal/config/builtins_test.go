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
	if len(registry.ChannelTypes) != 21 {
		t.Fatalf("unexpected built-in registry: %+v", registry)
	}
	for code, id := range map[string]uint64{
		"openai": 9000000000000001, "anthropic": 9000000000000008,
		"aws_bedrock": 9000000000000009, "gemini": 9000000000000010,
		"newapi": 9000000000000012, "qiniu": 9000000000000013, "siliconflow": 9000000000000014,
		"opencode_go": 9000000000000015, "openrouter": 9000000000000016, "zhipu": 9000000000000017,
		"zhipu_api": 9000000000000018,
		"volcengine_ark": 9000000000000004, "volcengine_ark_coding": 9000000000000019,
		"volcengine_ark_agent": 9000000000000020, "volcengine_ark_video": 9000000000000021,
		"commandcode": 9000000000000022,
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

func TestOpenCodeGoBuiltinDeclaresUsageQuota(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	item, exists := registry.ChannelTypeByCode("opencode_go")
	if !exists {
		t.Fatal("OpenCode Go channel type is missing")
	}
	var config struct {
		BaseURL string          `json:"baseUrl"`
		Quota   json.RawMessage `json:"quota"`
	}
	if err = json.Unmarshal(item.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("OpenCode Go base URL = %q", config.BaseURL)
	}
	if !strings.Contains(string(config.Quota), `"opencode_go_usage"`) {
		t.Fatalf("OpenCode Go quota config = %s, want opencode_go_usage adapter", config.Quota)
	}
	if !strings.Contains(string(config.Quota), `https://opencode.ai/zen/go/v1/usage`) {
		t.Fatalf("OpenCode Go quota path must be the absolute usage URL, got %s", config.Quota)
	}
}

func TestCommandCodeBuiltinPinsChatCompletions(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	item, exists := registry.ChannelTypeByCode("commandcode")
	if !exists {
		t.Fatal("Command Code channel type is missing")
	}
	var config struct {
		BaseURL  string          `json:"baseUrl"`
		Models   json.RawMessage `json:"models"`
		Costs    json.RawMessage `json:"costs"`
		Protocol json.RawMessage `json:"protocol"`
	}
	if err = json.Unmarshal(item.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://api.commandcode.ai/provider/v1" {
		t.Fatalf("Command Code base URL = %q", config.BaseURL)
	}
	if len(config.Models) == 0 {
		t.Fatal("Command Code must declare model discovery")
	}
	if !strings.Contains(string(config.Costs), "none") {
		t.Fatalf("Command Code costs config = %s, want none adapter", config.Costs)
	}
	var protocolConfig struct {
		ChatCompletionsOnly bool `json:"chatCompletionsOnly"`
	}
	if err = json.Unmarshal(config.Protocol, &protocolConfig); err != nil {
		t.Fatal(err)
	}
	if !protocolConfig.ChatCompletionsOnly {
		t.Fatalf("Command Code protocol config = %s, want chatCompletionsOnly", config.Protocol)
	}
}

func TestVolcengineArkPlanBuiltins(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		id          uint64
		name        string
		baseURL     string
		quotaAdapter string
	}{
		"volcengine_ark": {
			id: 9000000000000004, name: "火山方舟 Ark",
			baseURL: "https://ark.cn-beijing.volces.com/api/v3",
		},
		"volcengine_ark_coding": {
			id: 9000000000000019, name: "火山方舟 Coding Plan",
			baseURL: "https://ark.cn-beijing.volces.com/api/coding/v3",
		},
		"volcengine_ark_agent": {
			id: 9000000000000020, name: "火山方舟 Agent Plan",
			baseURL: "https://ark.cn-beijing.volces.com/api/plan/v3",
			quotaAdapter: "volcengine_afp",
		},
	}
	for code, want := range cases {
		item, exists := registry.ChannelTypeByCode(code)
		if !exists {
			t.Fatalf("channel type %s is missing", code)
		}
		if item.ID != want.id || item.Name != want.name {
			t.Fatalf("%s = id %d name %q, want id %d name %q", code, item.ID, item.Name, want.id, want.name)
		}
		var config struct {
			BaseURL string          `json:"baseUrl"`
			Quota   json.RawMessage `json:"quota"`
		}
		if err = json.Unmarshal(item.Config, &config); err != nil {
			t.Fatal(err)
		}
		if config.BaseURL != want.baseURL {
			t.Fatalf("%s base URL = %q, want %q", code, config.BaseURL, want.baseURL)
		}
		if want.quotaAdapter == "" {
			if len(config.Quota) != 0 {
				t.Fatalf("%s must not declare quota config, got %s", code, config.Quota)
			}
			continue
		}
		if !strings.Contains(string(config.Quota), `"`+want.quotaAdapter+`"`) {
			t.Fatalf("%s quota config = %s, want adapter %q", code, config.Quota, want.quotaAdapter)
		}
	}
}
