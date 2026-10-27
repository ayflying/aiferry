// Package schedule 是系统后台定时任务的统一登记与状态中心。
//
// 为什么要有这个包：后台任务原先散落在各业务包里各自起 goroutine，谁在跑、上次
// 什么时候跑的、下次什么时候跑、上一次成功还是失败，全部只能翻日志猜。运维要确认
// 「每日余额同步到底还活着吗」得去 grep 日志，而这恰恰是最需要一眼看清的信息。
//
// 这里提供一套统一契约：
//   - 任务启动时用 Register 登记自己的名称、说明、调度方式；
//   - 每轮执行前后调用 Run 上报开始与结果（成功/失败、耗时、错误摘要）；
//   - 管理端通过 Snapshot 拿到全部任务的当前状态，渲染成计划任务列表。
//
// 状态只存进程内内存，不落库：定时任务是进程级资源，多副本部署时每个副本各有自己
// 的调度，落库反而会互相覆盖出假象。要判断「整体是否正常运行」看这一份就够。
package schedule

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Kind 是任务的调度方式，决定「下次执行时间」怎么算以及界面上怎么描述。
type Kind string

const (
	// KindInterval 固定间隔重复执行（如每 10 秒巡检一次）。
	KindInterval Kind = "interval"
	// KindDailyAt 每天固定时刻执行（如每天 00:01 同步余额）。
	KindDailyAt Kind = "daily_at"
	// KindHourlyAfter 每小时检查、但只在当日指定小时之后才真正执行
	// （如签到：每小时看一次，6 点后才签，避免跨零点日切）。
	KindHourlyAfter Kind = "hourly_after"
	// KindEvent 由外部事件驱动、没有固定周期（如启动时立即执行一次）。
	KindEvent Kind = "event"
)

// Spec 是任务的静态描述，注册时一次性写入，之后不再变化。
type Spec struct {
	// Key 是任务唯一标识，前端用它做列表 key，一经发布不要随意改动。
	Key string
	// Name 是人类可读的任务名，界面上直接展示。
	Name string
	// Description 说明这个任务做什么、什么时候跑、有哪些前置条件。
	Description string
	Kind        Kind
	// Interval 是 KindInterval 的周期。
	Interval time.Duration
	// Hour / Minute 是 KindDailyAt 的执行时刻（服务端本地时区）。
	Hour int
	// Minute 是 KindDailyAt 的分钟。
	Minute int
	// AfterHour 是 KindHourlyAfter 的起始小时。
	AfterHour int
	// Enabled 表示任务当前是否真的在运行；false 时界面上应显示「未启用」并给出原因。
	Enabled bool
	// DisabledReason 说明为什么没运行（如「未开启报文记录」「保留天数未设置」）。
	DisabledReason string
}

// Status 是任务的运行时状态，由各任务在执行前后上报。
type Status struct {
	Spec
	// Running 表示当前是否有一轮正在执行。
	Running bool
	// LastStartedAt / LastFinishedAt 是上一轮的开始与结束时间。
	LastStartedAt  *time.Time
	LastFinishedAt *time.Time
	// LastDurationMs 是上一轮耗时（毫秒），便于发现任务悄悄变慢。
	LastDurationMs int64
	// LastError 是上一轮的失败原因；空表示上一轮成功。
	LastError string
	// RunCount / FailureCount 是进程启动以来的累计执行与失败次数。
	RunCount     int64
	FailureCount int64
	// NextRunAt 是根据调度规则推算的下次执行时间；KindEvent 恒为空。
	NextRunAt *time.Time
}

// registry 是注册中心本体。用一个互斥锁保护全部字段：任务数量只有个位数、
// 写操作也只在每轮执行前后发生，锁竞争可以忽略。
type registry struct {
	mu      sync.Mutex
	entries map[string]*Status
	// startedAt 是本进程启动时间，用于界面上判断「本次启动后还没跑过」。
	startedAt time.Time
}

var defaultRegistry = &registry{
	entries:   make(map[string]*Status),
	startedAt: time.Now(),
}

// Register 登记一个任务并返回其当前状态指针的副本操作入口。重复登记同一个 Key
// 会覆盖静态描述但保留运行状态，避免热重载或重复初始化时丢掉历史。
func Register(spec Spec) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	entry, exists := defaultRegistry.entries[spec.Key]
	if !exists {
		entry = &Status{}
		defaultRegistry.entries[spec.Key] = entry
	}
	entry.Spec = spec
	entry.NextRunAt = nextRun(spec, time.Now())
}

// Begin 在任务开始执行时调用，标记运行中并记录开始时间。
func Begin(key string) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	entry, exists := defaultRegistry.entries[key]
	if !exists {
		return
	}
	now := time.Now()
	entry.Running = true
	entry.LastStartedAt = &now
}

// Finish 在任务执行结束时调用。err 为 nil 表示成功。无论成败都会累加计数，
// 并按调度规则更新「下次执行时间」，让界面上的时间戳始终跟着真实执行滚动。
func Finish(key string, err error) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	entry, exists := defaultRegistry.entries[key]
	if !exists {
		return
	}
	now := time.Now()
	entry.Running = false
	entry.LastFinishedAt = &now
	entry.RunCount++
	if entry.LastStartedAt != nil {
		entry.LastDurationMs = now.Sub(*entry.LastStartedAt).Milliseconds()
	}
	if err != nil {
		entry.FailureCount++
		entry.LastError = err.Error()
	} else {
		entry.LastError = ""
	}
	entry.NextRunAt = nextRun(entry.Spec, now)
}

// Run 是 Begin/Finish 的组合封装，任务用它可以避免漏写 Finish 导致状态卡在
// 「运行中」。fn 返回的错误会被记录，随后原样抛出给调用方。
func Run(ctx context.Context, key string, fn func(context.Context) error) error {
	Begin(key)
	err := fn(ctx)
	Finish(key, err)
	return err
}

// Snapshot 返回全部任务的状态快照，按名称排序便于阅读。返回的是副本，
// 调用方随便改都不会影响注册中心。
func Snapshot() []Status {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	list := make([]Status, 0, len(defaultRegistry.entries))
	for _, entry := range defaultRegistry.entries {
		copied := *entry
		list = append(list, copied)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Enabled != list[j].Enabled {
			return list[i].Enabled
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// StartedAt 返回本进程启动时间，界面上用来解释「为什么还没跑过」。
func StartedAt() time.Time {
	return defaultRegistry.startedAt
}

// nextRun 按调度规则推算下一次执行时间。算不出来（KindEvent 或周期非法）返回 nil。
func nextRun(spec Spec, from time.Time) *time.Time {
	switch spec.Kind {
	case KindInterval:
		if spec.Interval <= 0 {
			return nil
		}
		next := from.Add(spec.Interval)
		return &next
	case KindDailyAt:
		next := time.Date(from.Year(), from.Month(), from.Day(), spec.Hour, spec.Minute, 0, 0, from.Location())
		if !next.After(from) {
			next = next.AddDate(0, 0, 1)
		}
		return &next
	case KindHourlyAfter:
		// 与任务实际逻辑一致：每小时整点检查一次，未到起始小时就等到该小时的
		// 巡检点。这里给的是「下一个可能真正执行的时刻」。
		candidate := from.Truncate(time.Hour).Add(time.Hour)
		if from.Hour() < spec.AfterHour && candidate.Hour() <= spec.AfterHour {
			candidate = time.Date(from.Year(), from.Month(), from.Day(), spec.AfterHour, 0, 0, 0, from.Location())
			if !candidate.After(from) {
				candidate = candidate.Add(time.Hour)
			}
		}
		return &candidate
	default:
		return nil
	}
}
