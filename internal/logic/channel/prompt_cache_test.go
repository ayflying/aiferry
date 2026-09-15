package channel

import (
	"encoding/json"
	"strings"
	"testing"
)

const promptCacheTestBody = `{"model":"deepseek-flash","messages":[{"role":"user","content":"hi","prompt_cache_breakpoint":{"mode":"explicit"}}],"prompt_cache_key":"client-key","prompt_cache_options":{"mode":"implicit"},"prompt_cache_retention":"24h","max_tokens":16}`

func decodePromptCacheBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	return payload
}

func TestResolvePromptCacheModeFollowsConfigAndLegacySwitch(t *testing.T) {
	cases := []struct {
		name   string
		config AdvancedConfig
		want   string
	}{
		{"缺省按稳定缓存键", AdvancedConfig{}, PromptCacheModeStable},
		{"显式 stable", AdvancedConfig{PromptCacheMode: PromptCacheModeStable}, PromptCacheModeStable},
		{"显式 off", AdvancedConfig{PromptCacheMode: PromptCacheModeOff}, PromptCacheModeOff},
		{"显式 passthrough", AdvancedConfig{PromptCacheMode: PromptCacheModePassthrough}, PromptCacheModePassthrough},
		{"非法值回落 stable", AdvancedConfig{PromptCacheMode: "unknown"}, PromptCacheModeStable},
		{"历史透传开关优先", AdvancedConfig{PromptCacheMode: PromptCacheModeOff, PassthroughPromptCache: true}, PromptCacheModePassthrough},
	}
	for _, item := range cases {
		if actual := item.config.ResolvePromptCacheMode(); actual != item.want {
			t.Fatalf("%s: ResolvePromptCacheMode() = %q, want %q", item.name, actual, item.want)
		}
	}
}

func TestApplyPromptCachePolicyStableInjectsSystemKey(t *testing.T) {
	body, err := ApplyPromptCachePolicy([]byte(promptCacheTestBody), DefaultAdvancedConfig(), "v1|u:42")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePromptCacheBody(t, body)
	if payload["prompt_cache_key"] != PromptCacheKey("v1|u:42") {
		t.Fatalf("stable mode must inject the system key: %s", body)
	}
	for _, field := range []string{"prompt_cache_options", "prompt_cache_retention"} {
		if _, exists := payload[field]; exists {
			t.Fatalf("stable mode must strip %s: %s", field, body)
		}
	}
	message := payload["messages"].([]any)[0].(map[string]any)
	if _, exists := message["prompt_cache_breakpoint"]; exists {
		t.Fatalf("stable mode must strip nested breakpoints: %s", body)
	}
	if payload["model"] != "deepseek-flash" || payload["max_tokens"] != float64(16) {
		t.Fatalf("stable mode must keep the rest of the body: %s", body)
	}
}

func TestApplyPromptCachePolicyOffStripsWithoutSendingKey(t *testing.T) {
	config := DefaultAdvancedConfig()
	config.PromptCacheMode = PromptCacheModeOff
	body, err := ApplyPromptCachePolicy([]byte(promptCacheTestBody), config, "v1|u:42")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePromptCacheBody(t, body)
	for _, field := range []string{"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"} {
		if _, exists := payload[field]; exists {
			t.Fatalf("off mode must strip %s: %s", field, body)
		}
	}
	if payload["model"] != "deepseek-flash" || payload["max_tokens"] != float64(16) {
		t.Fatalf("off mode must keep the rest of the body: %s", body)
	}
}

func TestApplyPromptCachePolicyPassthroughKeepsClientFields(t *testing.T) {
	config := DefaultAdvancedConfig()
	config.PromptCacheMode = PromptCacheModePassthrough
	body, err := ApplyPromptCachePolicy([]byte(promptCacheTestBody), config, "v1|u:42")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePromptCacheBody(t, body)
	if payload["prompt_cache_key"] != "client-key" || payload["prompt_cache_retention"] != "24h" {
		t.Fatalf("passthrough mode must keep client cache fields: %s", body)
	}
	message := payload["messages"].([]any)[0].(map[string]any)
	if _, exists := message["prompt_cache_breakpoint"]; !exists {
		t.Fatalf("passthrough mode must keep client breakpoints: %s", body)
	}
}

func TestApplyPromptCachePolicyStableWithoutIdentityOnlyStrips(t *testing.T) {
	body, err := ApplyPromptCachePolicy([]byte(promptCacheTestBody), DefaultAdvancedConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePromptCacheBody(t, body)
	if _, exists := payload["prompt_cache_key"]; exists {
		t.Fatalf("empty identity must not fabricate a cache key: %s", body)
	}
}

func TestApplyPromptCachePolicyRejectsInvalidBody(t *testing.T) {
	if _, err := ApplyPromptCachePolicy([]byte("not json"), DefaultAdvancedConfig(), "v1|u:42"); err == nil {
		t.Fatal("invalid JSON body should be rejected")
	}
}

func TestPromptCacheKeyIsStableAndScoped(t *testing.T) {
	key := PromptCacheKey("v1|u:42|m:deepseek|flash")
	if key != PromptCacheKey("v1|u:42|m:deepseek|flash") {
		t.Fatal("the same identity must derive the same key")
	}
	if key == PromptCacheKey("v1|u:43|m:deepseek|flash") {
		t.Fatal("different identities must not share a cache key")
	}
	if !strings.HasPrefix(key, "aiferry:") {
		t.Fatalf("unexpected key format: %s", key)
	}
}
