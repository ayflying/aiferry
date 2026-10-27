package schedule

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNextRunDailyAt 守住「每天定时」的下次执行推算：还没到今天的时刻就是今天，
// 已过就是明天，否则界面会把「明天 00:01」错误显示成「今天 00:01（即将执行）」。
func TestNextRunDailyAt(t *testing.T) {
	spec := Spec{Kind: KindDailyAt, Hour: 0, Minute: 1}
	location := time.FixedZone("CST", 8*60*60)

	before := time.Date(2026, 10, 27, 0, 0, 0, 0, location)
	next := nextRun(spec, before)
	if next == nil || !next.Equal(time.Date(2026, 10, 27, 0, 1, 0, 0, location)) {
		t.Fatalf("零点前应算当天 00:01，得到 %v", next)
	}

	after := time.Date(2026, 10, 27, 0, 2, 0, 0, location)
	next = nextRun(spec, after)
	if next == nil || !next.Equal(time.Date(2026, 10, 28, 0, 1, 0, 0, location)) {
		t.Fatalf("零点后应算次日 00:01，得到 %v", next)
	}
}

// TestNextRunHourlyAfter 守住「每小时检查但只在某小时后执行」的推算：
// 凌晨时段应直接跳到起始小时，而不是给出一个不会真正执行的整点。
func TestNextRunHourlyAfter(t *testing.T) {
	spec := Spec{Kind: KindHourlyAfter, Interval: time.Hour, AfterHour: 6}
	location := time.FixedZone("CST", 8*60*60)

	early := time.Date(2026, 10, 27, 3, 20, 0, 0, location)
	next := nextRun(spec, early)
	if next == nil || next.Hour() != 6 || next.Day() != 27 {
		t.Fatalf("凌晨应指向当天 6 点，得到 %v", next)
	}

	late := time.Date(2026, 10, 27, 9, 20, 0, 0, location)
	next = nextRun(spec, late)
	if next == nil || !next.Equal(time.Date(2026, 10, 27, 10, 0, 0, 0, location)) {
		t.Fatalf("已过起始小时应指向下一个整点，得到 %v", next)
	}
}

// TestNextRunEvent 事件驱动任务没有下次执行时间，必须返回 nil 而不是零值时间。
func TestNextRunEvent(t *testing.T) {
	if next := nextRun(Spec{Kind: KindEvent}, time.Now()); next != nil {
		t.Fatalf("事件驱动应返回 nil，得到 %v", next)
	}
}

// TestRunRecordsSuccessAndFailure 验证 Run 会正确记录成功/失败、计数与耗时，
// 这是计划任务列表里「上次结果」和「累计失败次数」的数据来源。
func TestRunRecordsSuccessAndFailure(t *testing.T) {
	key := "test.run_" + time.Now().Format("150405.000000")
	Register(Spec{Key: key, Name: "测试任务", Kind: KindInterval, Interval: time.Minute, Enabled: true})

	if err := Run(context.Background(), key, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("成功执行不应返回错误：%v", err)
	}
	status := findStatus(t, key)
	if status.RunCount != 1 || status.FailureCount != 0 || status.LastError != "" {
		t.Fatalf("成功后状态不对：%+v", status)
	}
	if status.LastFinishedAt == nil || status.Running {
		t.Fatal("成功执行后应记录结束时间且不在运行中")
	}
	if status.NextRunAt == nil {
		t.Fatal("固定间隔任务应算出下次执行时间")
	}

	sentinel := errors.New("上游超时")
	if err := Run(context.Background(), key, func(context.Context) error { return sentinel }); err != sentinel {
		t.Fatalf("失败应原样返回错误，得到 %v", err)
	}
	status = findStatus(t, key)
	if status.FailureCount != 1 || status.LastError == "" {
		t.Fatalf("失败后应记录原因：%+v", status)
	}
	// 失败也必须更新下次执行时间，否则界面上会一直停在过去的时刻。
	if status.NextRunAt == nil {
		t.Fatal("失败后也应更新下次执行时间")
	}
}

// TestHumanDuration 覆盖调度文案的格式化，避免出现「每 0 秒一次」这类误导文案。
func TestHumanDuration(t *testing.T) {
	cases := []struct {
		input time.Duration
		want  string
	}{
		{10 * time.Second, "10 秒"},
		{5 * time.Minute, "5 分钟"},
		{time.Hour, "1 小时"},
		{90 * time.Second, "90 秒"},
	}
	for _, item := range cases {
		if got := humanDuration(item.input); got != item.want {
			t.Fatalf("humanDuration(%v) = %q，期望 %q", item.input, got, item.want)
		}
	}
}

func findStatus(t *testing.T, key string) Status {
	t.Helper()
	for _, status := range Snapshot() {
		if status.Key == key {
			return status
		}
	}
	t.Fatalf("注册中心里找不到任务 %s", key)
	return Status{}
}
