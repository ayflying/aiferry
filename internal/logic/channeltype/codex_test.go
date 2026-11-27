package channeltype

import (
	"path/filepath"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
)

func TestCodexBuiltin(t *testing.T) {
	registry, err := config.LoadBuiltins(filepath.Join("..", "..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	item, ok := registry.ChannelTypeByCode("codex")
	if !ok || item.ID != 9000000000000025 {
		t.Fatalf("Codex builtin = %+v, exists=%v", item, ok)
	}
	cfg, err := ParseConfig(item.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://chatgpt.com/backend-api/codex" || cfg.Login.Adapter != AdapterLoginCodexDevice {
		t.Fatalf("Codex base/login = %s %+v", cfg.BaseURL, cfg.Login)
	}
	if cfg.Models.Method != "GET" || cfg.Models.Path != "/models?client_version=0.101.0" || cfg.Models.ListPath != "models" || cfg.Models.IDPath != "slug" {
		t.Fatalf("Codex models = %+v", cfg.Models)
	}
	if cfg.Quota.Adapter != AdapterCodexUsage || cfg.Quota.Method != "GET" || cfg.Quota.Path != "/backend-api/wham/usage" || cfg.Quota.AuthType != AuthChannelKey {
		t.Fatalf("Codex quota = %+v", cfg.Quota)
	}
	endpoint := cfg.Endpoints["responses"]
	if len(cfg.Endpoints) != 1 || endpoint.Method != "POST" || endpoint.Path != "/responses" || endpoint.RequestBody != "json" || !endpoint.SupportsStream || !cfg.Protocol.ForceUpstreamStream {
		t.Fatalf("Codex endpoints/protocol = %+v %+v", cfg.Endpoints, cfg.Protocol)
	}
	if cfg.Costs.Adapter != AdapterNone || cfg.Pricing.Adapter != AdapterNone {
		t.Fatalf("Codex must not invent monetary costs: %+v %+v", cfg.Costs, cfg.Pricing)
	}
}

func TestAntigravityConfigNormalization(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{"quota":{"adapter":" Antigravity_Usage "}}`))
	if err != nil {
		t.Fatal(err)
	}
	// 默认 GET + /quota + 渠道密钥 Bearer；路径按渠道根地址拼接（由 quota 解析层处理）。
	if cfg.Quota.Adapter != AdapterAntigravityUsage || cfg.Quota.Method != "GET" || cfg.Quota.Path != "/quota" || cfg.Quota.AuthType != AuthChannelKey || cfg.Quota.HeaderPrefix != "Bearer " {
		t.Fatalf("antigravity quota defaults = %+v", cfg.Quota)
	}
	// 鉴权由后端强制覆盖为渠道密钥：即使配置写成 management_key 也会被纠正而不是报错。
	if raw := `{"quota":{"adapter":"antigravity_usage","authType":"management_key"}}`; true {
		cfg, err := ParseConfig([]byte(raw))
		if err != nil || cfg.Quota.AuthType != AuthChannelKey {
			t.Fatalf("antigravity quota auth must be forced to channel key: %+v / %v", cfg.Quota, err)
		}
	}
	// 路径不以 / 开头且不是完整地址：拒绝，避免拼出错地址。
	if _, err := ParseConfig([]byte(`{"quota":{"adapter":"antigravity_usage","path":"quota"}}`)); err == nil {
		t.Fatal("relative quota path without leading slash accepted")
	}
}

func TestCodexConfigNormalization(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{"login":{"adapter":" Codex_Device ","platform":"unused","prefixPath":"unused"},"quota":{"adapter":" Codex_Usage "}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Login != (LoginConfig{Adapter: AdapterLoginCodexDevice}) {
		t.Fatalf("login = %+v", cfg.Login)
	}
	if cfg.Quota.Adapter != AdapterCodexUsage || cfg.Quota.Method != "GET" || cfg.Quota.Path != "/backend-api/wham/usage" || cfg.Quota.AuthType != AuthChannelKey || cfg.Quota.HeaderName != "Authorization" || cfg.Quota.HeaderPrefix != "Bearer " {
		t.Fatalf("quota defaults = %+v", cfg.Quota)
	}
	for _, raw := range []string{
		`{"quota":{"adapter":"codex_usage","method":"POST"}}`,
		`{"quota":{"adapter":"codex_usage","path":"backend-api/wham/usage"}}`,
		`{"quota":{"adapter":"codex_usage","authType":"oauth"}}`,
	} {
		if _, err := ParseConfig([]byte(raw)); err == nil {
			t.Fatalf("invalid quota accepted: %s", raw)
		}
	}
}
