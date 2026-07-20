package channel

import "testing"

func TestCredentialCostAvailabilityFor(t *testing.T) {
	positive := 0.01
	zero := 0.0
	negative := -0.01
	tests := []struct {
		name      string
		remaining *float64
		want      credentialCostAvailabilityAction
	}{
		{name: "余额未知不改变状态", remaining: nil, want: credentialCostAvailabilityNoChange},
		{name: "余额为零自动禁用", remaining: &zero, want: credentialCostAvailabilityDisable},
		{name: "负余额自动禁用", remaining: &negative, want: credentialCostAvailabilityDisable},
		{name: "正余额恢复自动禁用密钥", remaining: &positive, want: credentialCostAvailabilityRecover},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := credentialCostAvailabilityFor(test.remaining); got != test.want {
				t.Fatalf("credentialCostAvailabilityFor() = %d, want %d", got, test.want)
			}
		})
	}
}
