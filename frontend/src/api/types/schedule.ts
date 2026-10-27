/** 系统后台定时任务（计划任务）的运行状态。 */
export interface ScheduledTask {
  key: string
  name: string
  description: string
  /** 调度方式：interval 固定间隔 / daily_at 每天定时 / hourly_after 每小时检查但仅某时刻后执行 / event 事件驱动 */
  kind: 'interval' | 'daily_at' | 'hourly_after' | 'event'
  /** 调度规则的中文描述，由后端统一生成，避免前后端各写一套格式化 */
  scheduleText: string
  enabled: boolean
  /** 未启用时的原因（如未开启报文记录、未设置保留天数） */
  disabledReason?: string
  running: boolean
  /** 上次开始/结束时间；任务从未执行过时为空 */
  lastStartedAt?: string
  lastFinishedAt?: string
  lastDurationMs: number
  /** 上一轮的失败原因，为空表示成功 */
  lastError?: string
  runCount: number
  failureCount: number
  /** 下次执行时间；事件驱动任务为空 */
  nextRunAt?: string
}

export interface ScheduledTaskList {
  tasks: ScheduledTask[]
  /** 本进程启动时间，用于解释「为什么还没执行过」 */
  processStartedAt: string
  now: string
}
