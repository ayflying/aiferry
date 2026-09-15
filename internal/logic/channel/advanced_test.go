package channel

import (
	"strings"
	"testing"
)

func TestParseAdvancedConfigDefaultsToBlockingStore(t *testing.T) {
	config, err := ParseAdvancedConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !config.BlockStore || config.PassthroughRequestBody || config.PassthroughPromptCache || config.AllowServiceTier {
		t.Fatalf("unexpected default config: %+v", config)
	}
}

func TestParseAdvancedConfigKeepsExplicitStorePermission(t *testing.T) {
	config, err := ParseAdvancedConfig([]byte(`{"blockStore":false,"allowInclude":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.BlockStore || !config.AllowInclude {
		t.Fatalf("unexpected parsed config: %+v", config)
	}
}

func TestParseAdvancedConfigKeepsExplicitPromptCachePassthrough(t *testing.T) {
	config, err := ParseAdvancedConfig([]byte(`{"passthroughPromptCache":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !config.PassthroughPromptCache {
		t.Fatalf("prompt cache passthrough was not preserved: %+v", config)
	}
}

func TestParseAdvancedConfigValidatesPromptCacheMode(t *testing.T) {
	for _, raw := range []string{`{"promptCacheMode":""}`, `{"promptCacheMode":"stable"}`, `{"promptCacheMode":"off"}`, `{"promptCacheMode":"passthrough"}`} {
		config, err := ParseAdvancedConfig([]byte(raw))
		if err != nil {
			t.Fatalf("ParseAdvancedConfig(%s) error = %v", raw, err)
		}
		if config.ResolvePromptCacheMode() == "" {
			t.Fatalf("ParseAdvancedConfig(%s) resolved to an empty mode", raw)
		}
	}
	trimmed, err := ParseAdvancedConfig([]byte(`{"promptCacheMode":" off "}`))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if trimmed.PromptCacheMode != PromptCacheModeOff {
		t.Fatalf("prompt cache mode was not trimmed: %+v", trimmed)
	}
	if _, err = ParseAdvancedConfig([]byte(`{"promptCacheMode":"none"}`)); err == nil {
		t.Fatal("unknown prompt cache mode must be rejected")
	}
	missing, err := ParseAdvancedConfig(nil)
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if missing.ResolvePromptCacheMode() != PromptCacheModeStable {
		t.Fatalf("channels without the field must keep the stable default: %+v", missing)
	}
}

func TestParseAdvancedConfigIgnoresRetiredProtocolConversion(t *testing.T) {
	config, err := ParseAdvancedConfig([]byte(`{"enableProtocolConversion":false,"forceOpenAIFormat":true}`))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if !config.ForceOpenAIFormat {
		t.Fatal("ForceOpenAIFormat was not preserved")
	}
	encoded, err := MarshalAdvancedConfig(config)
	if err != nil {
		t.Fatalf("MarshalAdvancedConfig() error = %v", err)
	}
	if strings.Contains(encoded, "enableProtocolConversion") {
		t.Fatalf("legacy protocol conversion switch was retained: %s", encoded)
	}
}

func TestParseAdvancedConfigProtocolConversionThreeStates(t *testing.T) {
	inherited, err := ParseAdvancedConfig([]byte(`{"forceOpenAIFormat":true}`))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if inherited.ProtocolConversion != nil {
		t.Fatal("missing field must inherit the system setting")
	}
	disabled, err := ParseAdvancedConfig([]byte(`{"protocolConversion":false}`))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if disabled.ProtocolConversion == nil || *disabled.ProtocolConversion {
		t.Fatal("explicit false must disable conversion for the channel")
	}
	enabled, err := ParseAdvancedConfig([]byte(`{"protocolConversion":true}`))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if enabled.ProtocolConversion == nil || !*enabled.ProtocolConversion {
		t.Fatal("explicit true must enable conversion for the channel")
	}
}

func TestNormalizeBackupBaseURLs(t *testing.T) {
	urls, err := normalizeBackupBaseURLs([]string{"https://cdn-a.example.com/v1/", "https://primary.example.com/v1", "https://cdn-a.example.com/v1", "  "}, "https://primary.example.com/v1/")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://cdn-a.example.com/v1" {
		t.Fatalf("unexpected backup URLs: %#v", urls)
	}
	if _, err = normalizeBackupBaseURLs([]string{"not a URL"}, "https://primary.example.com/v1"); err == nil {
		t.Fatal("invalid backup URL should be rejected")
	}
}

func TestAdvancedConfigUpstreamBaseURLsUsesPrimaryThenBackups(t *testing.T) {
	config := AdvancedConfig{BackupBaseURLs: []string{"https://cdn-a.example.com/v1", "https://primary.example.com/v1", "https://cdn-a.example.com/v1", "https://cdn-b.example.com/v1/"}}
	urls := config.UpstreamBaseURLs("https://primary.example.com/v1/")
	want := []string{"https://primary.example.com/v1", "https://cdn-a.example.com/v1", "https://cdn-b.example.com/v1"}
	if len(urls) != len(want) {
		t.Fatalf("URLs = %#v, want %#v", urls, want)
	}
	for index := range want {
		if urls[index] != want[index] {
			t.Fatalf("URLs = %#v, want %#v", urls, want)
		}
	}
}
