package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/model/entity"
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

func TestSelectHealthCheckModelIDUsesConfiguredModelOrEnabledFallback(t *testing.T) {
	models := []entity.ChannelModels{{Id: 3}, {Id: 7}}
	if got := selectHealthCheckModelID(0, models); got != 3 {
		t.Fatalf("fallback model = %d, want 3", got)
	}
	if got := selectHealthCheckModelID(7, models); got != 7 {
		t.Fatalf("configured model = %d, want 7", got)
	}
	if got := selectHealthCheckModelID(9, models); got != 0 {
		t.Fatalf("missing configured model = %d, want 0", got)
	}
}
