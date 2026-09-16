package usage

import (
	"testing"
	"time"
)

func retentionTestLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}

func TestRetentionScheduledAtUsesLocalClock(t *testing.T) {
	location := retentionTestLocation()
	now := time.Date(2026, 9, 16, 4, 5, 6, 0, location)
	got := retentionScheduledAt(now)
	want := time.Date(2026, 9, 16, 3, 30, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("scheduled at %s, want %s", got, want)
	}
	if got.Location() != location {
		t.Fatalf("scheduled location %s, want %s", got.Location(), location)
	}
}

func TestRetentionDueOnlyRunsAfterScheduledTime(t *testing.T) {
	location := retentionTestLocation()
	day := func(hour, minute int) time.Time {
		return time.Date(2026, 9, 16, hour, minute, 0, 0, location)
	}
	tests := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		want    bool
	}{
		{name: "计划时刻之前不执行", now: day(3, 29), lastRun: time.Time{}, want: false},
		{name: "到点且今日未执行", now: day(3, 30), lastRun: time.Time{}, want: true},
		{name: "过点且今日未执行（补跑）", now: day(17, 0), lastRun: time.Time{}, want: true},
		{name: "今日已执行不重复", now: day(17, 0), lastRun: day(3, 31), want: false},
		{name: "昨日执行过今日仍要跑", now: day(3, 30), lastRun: day(3, 30).AddDate(0, 0, -1), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retentionDue(test.now, test.lastRun); got != test.want {
				t.Fatalf("retentionDue(%s, %s) = %v, want %v", test.now, test.lastRun, got, test.want)
			}
		})
	}
}

func TestRetentionCutoffAlignsToLocalMidnight(t *testing.T) {
	location := retentionTestLocation()
	now := time.Date(2026, 9, 16, 17, 24, 0, 0, location)
	got := retentionCutoff(now, 90)
	// 以本地当天 0 点为基准回推 90 天，再转 UTC 交给带 loc=Asia/Shanghai 的连接。
	want := time.Date(2026, 6, 17, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", got.UTC(), want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("cutoff location %s, want UTC", got.Location())
	}
}

func TestRetentionCutoffKeepsWallClockBoundary(t *testing.T) {
	location := retentionTestLocation()
	// 同一自然日内的不同时刻必须得到同一个窗口下界，
	// 否则清理范围会随当天执行时间漂移。
	early := retentionCutoff(time.Date(2026, 9, 16, 0, 1, 0, 0, location), 30)
	late := retentionCutoff(time.Date(2026, 9, 16, 23, 59, 0, 0, location), 30)
	if !early.Equal(late) {
		t.Fatalf("cutoff drifted within the same day: %s vs %s", early, late)
	}
}

func TestCleanupBeforeRejectsEmptyBounds(t *testing.T) {
	service := &sUsage{}
	for _, test := range []struct {
		name       string
		batchSize  int
		maxBatches int
	}{
		{name: "批大小为 0", batchSize: 0, maxBatches: 10},
		{name: "批大小为负", batchSize: -1, maxBatches: 10},
		{name: "批数为 0", batchSize: 100, maxBatches: 0},
		{name: "批数为负", batchSize: 100, maxBatches: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			deleted, err := service.CleanupBefore(nil, time.Now(), test.batchSize, 0, test.maxBatches)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if deleted != 0 {
				t.Fatalf("deleted %d rows, want 0 without touching the database", deleted)
			}
		})
	}
}
