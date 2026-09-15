package relay

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/system"
)

func scorePointer(score int) *int {
	return &score
}

// 健康分参与候选加权排序：满分/未知分数保持原权重（负载均衡行为不变），
// 低于阈值按分数比例降权，且权重不会降到 0（仍需保留被重试的机会）。
func TestCandidateWeightReflectsHealthScore(t *testing.T) {
	cases := []struct {
		name  string
		score *int
		want  uint64
	}{
		{name: "未知分数不降权", score: nil, want: 9},
		{name: "满分不降权", score: scorePointer(system.ModelHealthMaxScore), want: 9},
		{name: "等于阈值不降权", score: scorePointer(system.ModelHealthWeightThreshold), want: 9},
		{name: "阈值下一分按比例降权", score: scorePointer(system.ModelHealthWeightThreshold - 1), want: 8},
		{name: "半程分数降权一半", score: scorePointer(40), want: 4},
		{name: "极低分数保底为 1", score: scorePointer(1), want: 1},
		{name: "零分保底为 1", score: scorePointer(0), want: 1},
		{name: "负分保底为 1", score: scorePointer(-5), want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := Candidate{Weight: 9, HealthScore: tc.score}
			if got := candidateWeight(candidate); got != tc.want {
				t.Fatalf("candidateWeight(weight=9, score=%v) = %d, want %d", tc.score, got, tc.want)
			}
		})
	}
}

func TestCandidateWeightKeepsMinimumWeight(t *testing.T) {
	// 权重为 0 的候选同样按最低 1 计份额，与历史行为一致。
	if got := candidateWeight(Candidate{Weight: 0}); got != 1 {
		t.Fatalf("zero-weight candidate = %d, want 1", got)
	}
}

// 同一优先级组内的加权随机：健康的满分候选不应被淘汰出候选序列，
// 分数下滑的候选也必须仍然出现在结果里（只是份额更小）。
func TestWeightedOrderKeepsAllCandidates(t *testing.T) {
	candidates := []Candidate{
		{ChannelID: 9, Priority: 8, Weight: 9, HealthScore: scorePointer(100)},
		{ChannelID: 32, Priority: 8, Weight: 5, HealthScore: scorePointer(10)},
		{ChannelID: 31, Priority: 8, Weight: 5},
	}
	seen := make(map[uint64]int)
	for i := 0; i < 200; i++ {
		ordered := weightedOrder(candidates)
		if len(ordered) != len(candidates) {
			t.Fatalf("weightedOrder returned %d candidates, want %d", len(ordered), len(candidates))
		}
		seen[ordered[0].ChannelID]++
	}
	if len(seen) < 2 {
		t.Fatalf("only %v was ever selected first, weighting looks degenerate", seen)
	}
	if seen[9] <= seen[32] {
		t.Fatalf("最高健康分渠道未被优先选中: %v", seen)
	}
}
