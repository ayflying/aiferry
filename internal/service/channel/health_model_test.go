package channel

import (
	"strings"
	"testing"
)

func TestChannelAutoDisableEnabledDefaultsAndOverrides(t *testing.T) {
	if !channelAutoDisableEnabled(nil, true) || channelAutoDisableEnabled(nil, false) {
		t.Fatal("missing value should preserve the supplied default")
	}
	falseValue := false
	trueValue := true
	if channelAutoDisableEnabled(&trueValue, false) != true || channelAutoDisableEnabled(&falseValue, true) != false {
		t.Fatal("explicit value should override the supplied default")
	}
}

func TestHealthCheckModelJoinUsesConfiguredModelOrEnabledFallback(t *testing.T) {
	modelID := healthCheckModelIDExpression("c")
	if !strings.Contains(modelID, "COALESCE(c.health_check_model_id") ||
		!strings.Contains(modelID, "fallback.channel_id=c.id") ||
		!strings.Contains(modelID, "fallback.enabled=1") ||
		!strings.Contains(modelID, "ORDER BY fallback.id ASC LIMIT 1") {
		t.Fatalf("unexpected health-check model selection: %s", modelID)
	}
	join := healthCheckModelJoin("c", "m")
	if !strings.Contains(join, "m.id="+modelID) ||
		!strings.Contains(join, "m.channel_id=c.id") ||
		!strings.Contains(join, "m.enabled=1") {
		t.Fatalf("unexpected health-check model join: %s", join)
	}
}
