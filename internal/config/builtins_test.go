package config

import (
	"path/filepath"
	"testing"
)

func TestLoadBuiltins(t *testing.T) {
	registry, err := LoadBuiltins(filepath.Join("..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.ChannelTypes) != 11 {
		t.Fatalf("unexpected built-in registry: %+v", registry)
	}
	for code, id := range map[string]uint64{
		"openai": 9000000000000001, "anthropic": 9000000000000008,
		"aws_bedrock": 9000000000000009, "gemini": 9000000000000010,
		"jiapi": 9000000000000011,
	} {
		if item, exists := registry.ChannelTypeByCode(code); !exists || item.ID != id {
			t.Fatalf("built-in channel type is missing: %s %+v", code, item)
		}
	}
}
