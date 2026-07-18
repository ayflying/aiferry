package channel

import "testing"

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
