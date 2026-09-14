# 分时定价（价格规则的时段条件）

价格规则（`model_price_rules.conditions_json`）支持按生效时段切换费率，用于 DeepSeek 峰谷价这类
「同一模型不同时段不同单价」的场景。前端编辑器见 `PriceRuleEditor.vue` + `TimeWindowEditor.vue`。

## 匹配顺序

同一公开模型下的规则按 **priority 降序**逐条试匹配，命中第一条即返回（权重相同则 id 大者优先）。
每条规则评估三个维度，**未声明的维度一律视为不限**：

| 维度 | 条件字段 | 未声明时 |
| --- | --- | --- |
| 端点 | `endpoint` | 不限端点 |
| token 区间 | `inputTokens(AtMost/AtLeast)`、`outputTokens*`、`totalTokens*` | 不限区间 |
| 生效时段 | `time` | 不限时段 |

兜底语义：**没有时段的条件不会把规则挡在门外**，而是作为兜底规则参与匹配。例如

| 规则 | 权重 | 条件 | 费率（输入/输出，USD per 1M） |
| --- | --- | --- | --- |
| 高价区 | 100 | 工作日 09:00–12:00、14:00–18:00 | 0.3 / 1.2 |
| 空闲时段 | 80 | 无时段限制 | 0.15 / 0.6 |

→ 工作日 10:00 命中高价区；12:30、18:55、周六周日命中空闲时段。
反过来，如果带时段的高价区被当成「无条件命中」，就会 24 小时按高峰价计费——这是 0.5.113 之前的行为。

## `conditions.time` 结构

```json
{
  "tz": "Asia/Shanghai",
  "weekdays": [1, 2, 3, 4, 5],
  "ranges": [["09:00", "12:00"], ["14:00", "18:00"]]
}
```

| 字段 | 说明 |
| --- | --- |
| `tz` | IANA 时区名，缺省 `Asia/Shanghai`。容器本地时区通常是 UTC，不设默认值会让时段整体偏移 8 小时 |
| `weekdays` | ISO 8601 编号：1=周一 … 7=周日。空或七选七表示不限星期 |
| `ranges` | `["HH:MM","HH:MM"]` 数组，**左闭右开**。起止相同（含 `00:00–00:00`）表示全天；`start > end` 表示跨零点（如 `22:00–06:00`） |

等价于「不限」的写法（前端显示为「不限时段」）：完全没有 `time` 块、`time` 为 `null`、
只写 `tz`、只写七选七的 `weekdays`。这类规则按兜底规则处理，不会被时间条件挡住。

时段必须写成**补零的 `HH:MM`**（`"9:00"` 非法）；一天的上界用 `23:59` 或「起止相同」表达，
不支持 `24:00`。

## 评估时刻

- **结算用请求起始时刻**，不是写账单的时刻：跨时段的请求必须按它开始时的档位计费。
  调用链 `relay/usage.go`、`relay/audio.go` 把请求起始时刻作为 `at` 传下去。
- **历史账单重建用日志自身的 `created_at`**（`usage/legacy_billing.go`），否则会用「当前时段」
  的费率重算历史记录，与库中已存的 `estimated_cost` 对不上。
- 缓存只缓存**规则定义**，命中的是按请求实时计算，因此切换时段的瞬间不会沿用上一个时段的费率。

## 校验与容错

保存路径 `channel/pricing.go: validateConditions` 与计费路径共用 `timewindow.Parse`，
两边判定规则完全一致，不会出现「保存通过、运行时却不命中」的写法差异。

库中历史异常值（未知时区、未补零时段、`weekdays` 越界等）在计费时**判不命中**（fail-closed）：
反过来默认命中会把条件规则退化成全天生效的兜底规则，正是要避免的故障形态。

## 实现位置

| 环节 | 位置 |
| --- | --- |
| 解析 / 校验 / 判定 | `internal/logic/timewindow/window.go`：`Allows` = 条件是否适用于该时刻，`Contains` = 是否落在关闭窗口内 |
| 规则匹配 | `internal/logic/usage/pricing.go`：`RuleMatches` / `RuleBreakdown` / `EstimateRuleBreakdown` |
| 缓存层入口 | `internal/logic/pricingcache/service.go`：`Estimate` / `EstimateBreakdown` |
| 保存校验 | `internal/logic/channel/pricing.go`：`validateConditions` |

`timewindow` 同时服务「模型分时定价」和「渠道模型定时关闭」（`docs/model-closed-windows.md`），
两处的时区、星期编号、跨零点规则共用一份实现。两者的语义差别集中在「没有时段」这一支：
关闭窗口要求有时段才判定（`Contains`），定价条件则视为不限（`Allows`）。

## 已知限制

上游价格同步链路目前仍只产出非高峰档费率：计费表达式到价格规则的转换没有把时段写进条件分支，
因此 deepseek 系的同步价偏低。时段判定已可用，恢复峰谷档是同步侧的后续改动。
