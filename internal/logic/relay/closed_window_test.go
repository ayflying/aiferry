package relay

import (
	"testing"
	"time"
)

func relayTestTime(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Shanghai) error = %v", err)
	}
	return time.Date(year, month, day, hour, minute, 0, 0, location)
}

func TestFilterClosedCandidatesDropsOnlyMatchingWindow(t *testing.T) {
	// 2026-09-14 是周一。
	at := relayTestTime(t, 2026, time.September, 14, 10, 0)
	candidates := []Candidate{
		{ChannelModelID: 1, PublicName: "open-model"},
		{ChannelModelID: 2, PublicName: "closed-model", ClosedWindow: `{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["09:00","12:00"]]}`},
		{ChannelModelID: 3, PublicName: "off-peak-model", ClosedWindow: `{"tz":"Asia/Shanghai","weekdays":[1,2,3,4,5],"ranges":[["14:00","18:00"]]}`},
	}
	available := filterClosedCandidates(candidates, at)
	if len(available) != 2 {
		t.Fatalf("可用候选数 = %d, want 2（%+v）", len(available), available)
	}
	for _, candidate := range available {
		if candidate.ChannelModelID == 2 {
			t.Fatalf("处于关闭时段的候选不应保留：%+v", candidate)
		}
	}
}

func TestFilterClosedCandidatesKeepsInputSliceIntact(t *testing.T) {
	// 候选择片可能直接来自 Redis 缓存反序列化结果，过滤不能就地改写它，
	// 否则缓存对象会被污染、关闭时段结束后候选也回不来。
	at := relayTestTime(t, 2026, time.September, 14, 10, 0)
	candidates := []Candidate{
		{ChannelModelID: 1, ClosedWindow: `{"tz":"Asia/Shanghai","ranges":[["09:00","12:00"]]}`},
		{ChannelModelID: 2},
	}
	available := filterClosedCandidates(candidates, at)
	if len(available) != 1 || available[0].ChannelModelID != 2 {
		t.Fatalf("过滤结果异常：%+v", available)
	}
	if len(candidates) != 2 || candidates[0].ChannelModelID != 1 {
		t.Fatalf("入参切片被就地改写：%+v", candidates)
	}
}

func TestFilterClosedCandidatesFailsOpenOnBrokenWindow(t *testing.T) {
	at := relayTestTime(t, 2026, time.September, 14, 10, 0)
	candidates := []Candidate{
		{ChannelModelID: 1, ClosedWindow: `{"tz":`},
		{ChannelModelID: 2, ClosedWindow: `{"ranges":[["09:00"]]}`},
	}
	if available := filterClosedCandidates(candidates, at); len(available) != 2 {
		t.Fatalf("脏时段数据应放行而不是摘掉候选：%+v", available)
	}
}

func TestFilterClosedCandidatesHandlesEmptyInput(t *testing.T) {
	if available := filterClosedCandidates(nil, time.Now()); available != nil {
		t.Fatalf("空输入应原样返回：%+v", available)
	}
}
