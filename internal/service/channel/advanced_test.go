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
