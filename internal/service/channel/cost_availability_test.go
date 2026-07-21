package channel

import (
	"testing"
	"time"
)

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

func TestStoredCostAllowsCredentialRecovery(t *testing.T) {
	disabledAt := time.Date(2026, time.July, 21, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		remaining  float64
		disabledAt time.Time
		costAt     time.Time
		want       bool
	}{
		{name: "新正余额允许恢复", remaining: 1, disabledAt: disabledAt, costAt: disabledAt.Add(time.Minute), want: true},
		{name: "同时写入允许恢复", remaining: 1, disabledAt: disabledAt, costAt: disabledAt, want: true},
		{name: "旧余额不能恢复", remaining: 1, disabledAt: disabledAt, costAt: disabledAt.Add(-time.Minute), want: false},
		{name: "零余额不能恢复", remaining: 0, disabledAt: disabledAt, costAt: disabledAt.Add(time.Minute), want: false},
		{name: "手动停用不能恢复", remaining: 1, costAt: disabledAt.Add(time.Minute), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := storedCostAllowsCredentialRecovery(test.remaining, test.disabledAt, test.costAt); got != test.want {
				t.Fatalf("storedCostAllowsCredentialRecovery() = %t, want %t", got, test.want)
			}
		})
	}
}
