package relay

import (
	"time"

	"github.com/yunloli/aiferry/internal/logic/timewindow"
)

// 渠道模型的「定时关闭」在路由阶段生效：命中关闭时段的候选被摘掉，
// 其余候选照常参与加权与重试；全部候选都被关掉时，上层沿用既有
// 「no available channel」语义返回，不做隐式降级。
//
// 关闭时段随时间变化，而路由候选会被缓存到 Redis，因此判定必须发生在
// 缓存之外：缓存里保存的是未过滤的静态候选，命不命中缓存都要过这里。

// filterClosedCandidates 摘掉当前时刻处于关闭时段的候选。
// 入参切片可能直接来自缓存反序列化结果，这里始终返回新切片，不修改入参。
func filterClosedCandidates(candidates []Candidate, at time.Time) []Candidate {
	if len(candidates) == 0 {
		return candidates
	}
	available := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidateClosedAt(candidate, at) {
			continue
		}
		available = append(available, candidate)
	}
	return available
}

// candidateClosedAt 判断单个候选在给定时刻是否被定时关闭。
// 时段 JSON 异常时按未关闭处理（fail-open）：配置脏数据不应静默摘掉可用模型。
func candidateClosedAt(candidate Candidate, at time.Time) bool {
	if candidate.ClosedWindow == "" {
		return false
	}
	window, err := timewindow.Parse(candidate.ClosedWindow)
	if err != nil {
		return false
	}
	return window.Contains(at)
}
