package system

import (
	"testing"
	"time"
)

func TestNormalizeStatusCodeRules(t *testing.T) {
	normalized, rules, err := normalizeStatusCodeRules("429, 500-503,401,429")
	if err != nil || normalized != "401,429,500-503" || len(rules) != 3 {
		t.Fatalf("unexpected normalized rules: %q %#v %v", normalized, rules, err)
	}
	if !MatchesStatusCodeRules(normalized, 502) || MatchesStatusCodeRules(normalized, 422) {
		t.Fatal("status range matching is incorrect")
	}
}

func TestMatchesAutoDisable(t *testing.T) {
	settings := DefaultResilienceSettings()
	if !matchesAutoDisable(settings, AutoDisableInput{Status: 429}) {
		t.Fatal("configured status should disable a channel")
	}
	if !matchesAutoDisable(settings, AutoDisableInput{Latency: 120 * time.Second}) {
		t.Fatal("configured latency should disable a channel")
	}
	if !matchesAutoDisable(settings, AutoDisableInput{Message: "Daily usage limit exceeded"}) {
		t.Fatal("configured error keyword should disable a channel")
	}
	if matchesAutoDisable(settings, AutoDisableInput{Status: 400, Latency: time.Second, Message: "validation failed"}) {
		t.Fatal("unconfigured error should not disable a channel")
	}
}
