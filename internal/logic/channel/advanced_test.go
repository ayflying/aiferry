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
	if !config.BlockStore || config.PassthroughRequestBody || config.AllowServiceTier {
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
