# 计划任务可视化

管理端「计划任务」页面（`/scheduled-tasks`）集中展示系统全部后台定时任务：调度规则、
上次执行时间、下次执行时间、耗时、累计次数和失败原因。

## 为什么要有这个页面

后台任务原先散落在各业务包里各自起 goroutine。谁在跑、上次什么时候跑的、下一轮什么时候
跑、上一次是成功还是失败，全部只能翻日志推断。最典型的场景是「每日余额同步今天到底跑没
跑」——这类问题本该一眼看清，却要靠 grep 日志，而日志还会随容器重建丢失。

现在所有定时任务都通过 `internal/logic/schedule` 统一登记与上报，页面读的就是这份登记。

## 当前登记的任务

| 任务 | 调度 | 说明 |
|---|---|---|
| 每日余额同步 | 每天 00:01（北京时间） | 拉取各渠道上游费用与余额，失败按 5、10 分钟重试两轮；服务启动时先跑一次 |
| 渠道健康巡检 | 按系统设置里的间隔 | 重测被禁用的渠道/模型/密钥并尝试恢复，同时主动巡检正常渠道 |
| WorkBuddy 每日签到 | 每小时检查，仅 6 点后执行 | 遍历所有 WorkBuddy 渠道的每个账号，未签到的自动领取；启动 90 秒后补跑一轮 |
| 用量明细保留清理 | 每天 03:30 | 删除超过保留天数的调用明细；**默认关闭**，需设置 `USAGE_RETENTION_DAYS` |
| 请求报文文件清理 | 每小时 | 按修改时间删除旧报文，保证文件数不超上限；**默认关闭**，需开 `PAYLOAD_LOG_ENABLED` |

未启用的任务也会出现在列表里，并显示未启用原因——「为什么这个任务不跑」和「任务跑了但
失败」是两类完全不同的故障，界面上必须能区分。

## 设计要点

**状态只存内存，不落库**。定时任务是进程级资源，多副本部署时每个副本各有自己的调度；
把状态写进数据库反而会互相覆盖出假象。页面反映的是「当前这个副本」的真实情况，
这恰好是排查问题需要知道的东西。

**下次执行时间随每次执行滚动**。`Finish` 无论成功还是失败都会按调度规则重算下次时间。
失败不更新的话，界面会一直停在过去的时刻，看起来像任务卡死了。

**`schedule.Run` 包住整轮执行**。任务执行统一走 `schedule.Run(ctx, key, fn)`，
开始置「执行中」、结束记结果，避免漏写收尾导致状态永久卡在运行中。

**时间字段用指针而非零值**。任务还没执行过时返回 `nil`，前端显示「尚未执行」；
传零值时间会被渲染成 0001 年，比不显示更糟。

**调度文案由后端生成**（`ScheduleText`），不让前后端各写一套格式化逻辑，避免两处漂移。

## 新增任务的方式

在任务的启动函数里登记并让执行走 `Run`：

```go
schedule.Register(schedule.Spec{
    Key:         "your.task_key",
    Name:        "任务名称",
    Description: "这个任务做什么、什么时候跑、有什么前置条件。",
    Kind:        schedule.KindDailyAt,
    Hour:        2,
    Minute:      30,
    Enabled:     true, // 或按配置决定，false 时填 DisabledReason
})

go func() {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case now := <-ticker.C:
            _ = schedule.Run(ctx, "your.task_key", func(ctx context.Context) error {
                return doWork(ctx)
            })
        }
    }
}()
```

`Kind` 支持四种：`interval`（固定间隔）、`daily_at`（每天定时）、`hourly_after`
（每小时检查但仅某时刻后执行）、`event`（事件驱动，无下次执行时间）。

## 接口

`GET /api/admin/scheduled-tasks`（需管理员）返回：

```json
{
  "tasks": [
    {
      "key": "channel.workbuddy_checkin",
      "name": "WorkBuddy 每日签到",
      "scheduleText": "每 1 小时 检查一次，仅每天 06 点后执行",
      "enabled": true,
      "running": false,
      "lastFinishedAt": "2026-10-27T16:55:43+08:00",
      "lastDurationMs": 102,
      "nextRunAt": "2026-10-27T17:00:00+08:00",
      "runCount": 3,
      "failureCount": 0
    }
  ],
  "processStartedAt": "2026-10-27T16:53:11+08:00",
  "now": "2026-10-27T17:55:36+08:00"
}
```

`processStartedAt` 用来解释「为什么这个任务还没执行过」：进程刚启动时任务本来就没机会跑，
不该被误判成故障。前端只在进程已启动超过 15 分钟且仍无执行记录时才提示异常。

## 验证

`go test ./internal/logic/schedule/` 覆盖下次执行时间推算（跨日、凌晨跳过、事件驱动返回空）、
成功/失败状态记录、文案格式化。
