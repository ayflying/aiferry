# 失败请求的成本展示

## 问题

管理端用量页里，失败的请求（如 502）在「估算成本」列显示 **未定价 / 无价格快照**。
这是两处认定叠加的结果：

1. 转发侧只对 2xx 响应落计时快照（`pricedUsageCost` 走的是成功分支），
   失败记录 `estimated_cost` 为空、`billing_details_json` 为空；
2. 前端 `formatCost(undefined)` 直接返回「未定价」。

但失败不等于零成本：上游多数时候在错误响应里**仍然返回 usage**，
这几十万输入 token 是真实消耗的。把它显示成「未定价」既误导成本统计，
也让人以为系统漏算了。

## 口径

| 记录 | `estimated_cost` | 计费快照 | 界面 |
| --- | --- | --- | --- |
| 成功且计费 | 真实金额 | 有，`charged` 按实际扣款 | 金额 + 单价 |
| 失败且有价格快照 | **标记价** `1e-8` | 有，`billingMode=failed`、`charged=false` | 快照里的真实金额 +「已计费」 |
| 失败且无用量/无价格 | `NULL` | 无 | `$0.00` |

三条关键设计：

- **标记价故意非 0**。库里 0 与「这条记录没解析到用量」完全同形，
  用 0 当记号会把成功的免费调用、历史脏数据一起改写。
- **标记价必须按列的精度取值**。`estimated_cost` 是 `decimal(20,8)`，
  最初取 `1e-9`，落库时被 MySQL 直接舍入成 `0.00000000`，标记彻底失效
  （已在 243 上实测复现：写 `1e-9` 读回 `0.00000000`）。
  现取 `1e-8`——这一列能表示的最小非零值。前端
  `FAILURE_COST_SENTINEL_CEILING = 1e-8` 是同一判定线。
- **失败不扣款**。快照里 `charged=false`，用户余额、APIKey 累计消费都不变；
  失败请求的标记价不进入任何 SUM 累加（相对真实金额可以忽略），
  但真实金额可通过快照还原，供列表与详情展示。
- **失败成本只影响展示，不影响计费语义**。`billable` 分支的扣款逻辑一行未改。

## 实现位置

- `internal/logic/usage/failure_billing.go`
  - `FailureCost()` —— 标记价，转发侧、音频侧、渠道测试侧共用同一常量；
  - `NewFailedBillingDetails()` —— 把估算快照转成失败快照（丢弃数量为 0 的按量项，
    标注 `failed` 且 `charged=false`）；
  - `reconstructFailureBillingDetails()` —— 把**历史**失败记录的快照回填进列表响应，
    只读内存、不写库，条件是 `estimated_cost` 非空且状态码非 2xx。
- `internal/logic/relay/usage.go` —— `failedUsageBilling()`：非 `billable` 时替换成本口径。
- `internal/logic/relay/audio.go` —— `audioCost(status, ...)` / `audioDetails(status, ...)`。
- `internal/logic/channel/test_billing.go` —— 与转发侧共用同一口径。
- `frontend/src/lib/failure-cost.ts` —— `failureCostView()` / `formatFailureCost()` /
  `isFailureButBilled()`；页面不再各自判断「有没有快照」。
- `frontend/src/views/UsageView.vue` —— 失败行显示快照金额并加「已计费」标记。
- `frontend/src/components/UsageDetailDialog.vue` —— 计费方式显示「按 Token（失败请求）」，
  新增「扣款状态」行说明未扣款。

## 边界

- 失败请求若上游没返回 usage（如 502 期间连接直接失败），没有任何金额可算，
  界面显示 `$0.00`——这是「确实没有成本」，不是「未定价」。
- 历史失败记录的快照回填依赖当前价格表；价格被删改后回填会失败，
  此时仍显示 `$0.00`，不会伪造金额。
- 失败标记价仍可能出现在 API 的 `estimatedCost` 原值里；
  所有展示都必须经过 `failureCostView`/`formatFailureCost`，不能直接 `formatCost(row.estimatedCost)`。

## 测试

- `internal/logic/usage/failure_billing_test.go` —— 标记价非 0、能在 `decimal(20,8)`
  精度下原样落库（`TestFailureCostSurvivesDatabaseRoundTrip`，防 1e-9 被舍入成 0 的回归）、
  快照 `charged=false`、零用量项剔除、JSON 往返、失败分类判定。
- `frontend/src/lib/failure-cost.test.ts` —— 快照金额优先于标记价、无快照回落 `$0.00`、
  「已计费」标记只出现在失败且带快照的行、真实小额成本不被误判成标记。
- `frontend/src/views/UsageView.cost.test.ts` —— 用后端真实落库形状（502 +
  `estimated_cost=0.00000001` + `failed` 快照）驱动页面，断言 `costLabel` 返回 `$6.00`、
  `costBadge` 返回「已计费」，且任何情况下都不出现「未定价」。

## 验证记录

243（`192.168.50.243`）隔离环境实测，mock 上游对同一请求返回 502 但带
`prompt_tokens=1000000`、`completion_tokens=500000`，价格 2/8 每百万：

| id | http | estimated_cost | 快照 total | billingMode | charged |
| --- | --- | --- | --- | --- | --- |
| 3 | 200 | 6.00000000 | 6.00000000 | token | true |
| 4 | 502 | 0.00000000 | 6.00000000 | failed | false |
| 5 | 502 | 0.00000001 | 6.00000000 | failed | false |

id=4 是修正前（`1e-9` 被舍入成 0）的记录，id=5 是修正后的记录。
两者快照都带真实金额，界面都显示 `$6.00`，且都未扣款。
