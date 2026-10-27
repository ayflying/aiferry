package schedule

import (
	"strconv"
	"time"
)

// View 是计划任务在管理端的展示结构。时间字段用指针：任务还没跑过时前端要能
// 显示「尚未执行」，传零值时间会被误读成 0001 年。
type View struct {
	Key            string     `json:"key"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Kind           string     `json:"kind"`
	ScheduleText   string     `json:"scheduleText"`
	Enabled        bool       `json:"enabled"`
	DisabledReason string     `json:"disabledReason,omitempty"`
	Running        bool       `json:"running"`
	LastStartedAt  *time.Time `json:"lastStartedAt,omitempty"`
	LastFinishedAt *time.Time `json:"lastFinishedAt,omitempty"`
	LastDurationMs int64      `json:"lastDurationMs"`
	LastError      string     `json:"lastError,omitempty"`
	RunCount       int64      `json:"runCount"`
	FailureCount   int64      `json:"failureCount"`
	NextRunAt      *time.Time `json:"nextRunAt,omitempty"`
}

// ListResult 是列表接口的返回体。ProcessStartedAt 让界面能解释「为什么这个任务
// 还没跑过」——进程刚启动时任务本来就没机会执行，不该被误判成故障。
type ListResult struct {
	Tasks            []View    `json:"tasks"`
	ProcessStartedAt time.Time `json:"processStartedAt"`
	Now              time.Time `json:"now"`
}

// List 返回全部计划任务的可视化视图，按启用优先、名称排序（由 Snapshot 保证）。
func List() ListResult {
	tasks := Snapshot()
	views := make([]View, 0, len(tasks))
	for _, task := range tasks {
		views = append(views, View{
			Key:            task.Key,
			Name:           task.Name,
			Description:    task.Description,
			Kind:           string(task.Kind),
			ScheduleText:   scheduleText(task.Spec),
			Enabled:        task.Enabled,
			DisabledReason: task.DisabledReason,
			Running:        task.Running,
			LastStartedAt:  task.LastStartedAt,
			LastFinishedAt: task.LastFinishedAt,
			LastDurationMs: task.LastDurationMs,
			LastError:      task.LastError,
			RunCount:       task.RunCount,
			FailureCount:   task.FailureCount,
			NextRunAt:      task.NextRunAt,
		})
	}
	return ListResult{Tasks: views, ProcessStartedAt: StartedAt(), Now: time.Now()}
}

// scheduleText 把调度规则翻译成中文描述，避免界面各写一套格式化逻辑。
func scheduleText(spec Spec) string {
	switch spec.Kind {
	case KindInterval:
		if spec.Interval <= 0 {
			return "周期未设置"
		}
		return "每" + humanDuration(spec.Interval) + "一次"
	case KindDailyAt:
		return "每天 " + twoDigits(spec.Hour) + ":" + twoDigits(spec.Minute)
	case KindHourlyAfter:
		return "每" + humanDuration(spec.Interval) + "检查一次，仅每天 " + twoDigits(spec.AfterHour) + " 点后执行"
	default:
		return "事件驱动（启动时执行）"
	}
}

// humanDuration 把时长渲染成「10 秒」「5 分钟」「1 小时」这类可读文本。
// 刻意不在数字与单位之间留空格，因为调用方直接拼在「每」与「一次」中间，
// 留空格会拼出「每 10 秒 一次」这种读起来别扭的文本。
func humanDuration(value time.Duration) string {
	switch {
	case value >= time.Hour && value%time.Hour == 0:
		return strconv.Itoa(int(value/time.Hour)) + "小时"
	case value >= time.Minute && value%time.Minute == 0:
		return strconv.Itoa(int(value/time.Minute)) + "分钟"
	default:
		return strconv.Itoa(int(value/time.Second)) + "秒"
	}
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}
