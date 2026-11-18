package system

import (
	"testing"
	"time"

	"github.com/gogf/gf/v2/os/gtime"
)

func TestComboCooldownActive(t *testing.T) {
	now := gtime.New(time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC))
	future := gtime.New(now.Add(5 * time.Minute).Time)
	past := gtime.New(now.Add(-5 * time.Minute).Time)
	if comboCooldownActive(nil, now) {
		t.Fatal("没有冷却时间不应视为隔离中")
	}
	if !comboCooldownActive(future, now) {
		t.Fatal("冷却时间在将来应视为隔离中")
	}
	if comboCooldownActive(past, now) {
		t.Fatal("冷却时间已过应视为解除隔离")
	}
}

// 模型可用性按「模型 × 密钥」组合判断：只要还有一把启用密钥没被隔离，模型就仍可用。
func TestModelComboAvailable(t *testing.T) {
	now := gtime.New(time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC))
	future := gtime.New(now.Add(5 * time.Minute).Time)
	past := gtime.New(now.Add(-5 * time.Minute).Time)
	active := map[uint64]struct{}{101: {}, 102: {}}

	cases := []struct {
		name      string
		active    map[uint64]struct{}
		cooldowns map[uint64]map[uint64]*gtime.Time
		want      bool
	}{
		{"渠道没有任何启用密钥", map[uint64]struct{}{}, nil, false},
		{"模型还没有组合行", active, nil, true},
		{"两把密钥的组合都被隔离", active, map[uint64]map[uint64]*gtime.Time{7: {101: future, 102: future}}, false},
		{"两把密钥里还有一把可用", active, map[uint64]map[uint64]*gtime.Time{7: {101: future, 102: past}}, true},
		{"只记录了被隔离的组合", active, map[uint64]map[uint64]*gtime.Time{7: {101: future}}, true},
		{"冷却刚好到期", active, map[uint64]map[uint64]*gtime.Time{7: {101: past, 102: past}}, true},
	}
	for _, test := range cases {
		if got := modelComboAvailable(7, test.active, test.cooldowns, now); got != test.want {
			t.Fatalf("%s: modelComboAvailable() = %t, want %t", test.name, got, test.want)
		}
	}
}

// 统计只看可用模型，禁用或全部隔离的模型不计入。
func TestCountAvailableModels(t *testing.T) {
	items := []modelAvailability{
		{ModelID: 1, Enabled: true, Available: true},
		{ModelID: 2, Enabled: true, Available: false},
		{ModelID: 3, Enabled: false, Available: false},
	}
	if got := countAvailableModels(items); got != 1 {
		t.Fatalf("countAvailableModels() = %d, want 1", got)
	}
	if got := countAvailableModels(nil); got != 0 {
		t.Fatalf("空列表应为 0，实际 %d", got)
	}
}
