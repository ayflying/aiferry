package channel

import (
	"strings"
	"testing"
)

func TestParseAdvancedConfigReadsConcurrencyLimit(t *testing.T) {
	config, err := ParseAdvancedConfig([]byte(`{"concurrencyLimit":15}`))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if config.ConcurrencyLimit != 15 {
		t.Fatalf("ConcurrencyLimit = %d, want 15", config.ConcurrencyLimit)
	}
}

func TestParseAdvancedConfigDefaultsConcurrencyLimitToUnlimited(t *testing.T) {
	config, err := ParseAdvancedConfig(nil)
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if config.ConcurrencyLimit != 0 {
		t.Fatalf("ConcurrencyLimit = %d, want 0 (unlimited)", config.ConcurrencyLimit)
	}
}

func TestParseAdvancedConfigRejectsInvalidConcurrencyLimit(t *testing.T) {
	for _, raw := range []string{`{"concurrencyLimit":-1}`, `{"concurrencyLimit":1025}`} {
		if _, err := ParseAdvancedConfig([]byte(raw)); err == nil {
			t.Fatalf("ParseAdvancedConfig(%s) should be rejected", raw)
		}
	}
}

func TestMarshalAdvancedConfigKeepsConcurrencyLimit(t *testing.T) {
	encoded, err := MarshalAdvancedConfig(AdvancedConfig{ConcurrencyLimit: 8, BlockStore: true})
	if err != nil {
		t.Fatalf("MarshalAdvancedConfig() error = %v", err)
	}
	if !strings.Contains(encoded, `"concurrencyLimit":8`) {
		t.Fatalf("concurrency limit was not persisted: %s", encoded)
	}
	config, err := ParseAdvancedConfig([]byte(encoded))
	if err != nil {
		t.Fatalf("ParseAdvancedConfig() error = %v", err)
	}
	if config.ConcurrencyLimit != 8 {
		t.Fatalf("round-trip ConcurrencyLimit = %d, want 8", config.ConcurrencyLimit)
	}
}
