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
	if len(registry.ChannelTypes) != 7 {
		t.Fatalf("unexpected built-in registry: %+v", registry)
	}
	if item, exists := registry.ChannelTypeByCode("openai"); !exists || item.ID != 9000000000000001 {
		t.Fatalf("OpenAI channel type is missing: %+v", item)
	}
}
