package channel

import (
	"testing"
	"time"

	"github.com/gogf/gf/v2/os/gtime"
)

func TestComboRecoveryDue(t *testing.T) {
	base := time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	now := gtime.New(base)
	expired := gtime.New(base.Add(-time.Minute))
	pending := gtime.New(base.Add(time.Minute))

	tests := []struct {
		name          string
		healthScore   int
		cooldownUntil *gtime.Time
		want          bool
	}{
		{name: "隔离且无冷却", healthScore: 0, cooldownUntil: nil, want: true},
		{name: "隔离且冷却已到期", healthScore: 0, cooldownUntil: expired, want: true},
		{name: "隔离但冷却未到期", healthScore: 0, cooldownUntil: pending, want: false},
		{name: "日配额冷却未到期", healthScore: 100, cooldownUntil: pending, want: false},
		{name: "健康组合不重测", healthScore: 95, cooldownUntil: nil, want: false},
		{name: "健康组合冷却已到期也不重测", healthScore: 100, cooldownUntil: expired, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := comboRecoveryDue(test.healthScore, test.cooldownUntil, now); got != test.want {
				t.Fatalf("comboRecoveryDue(%d, %v, now) = %t, want %t", test.healthScore, test.cooldownUntil, got, test.want)
			}
		})
	}
}
