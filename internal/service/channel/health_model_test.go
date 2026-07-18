package channel

import "testing"

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
