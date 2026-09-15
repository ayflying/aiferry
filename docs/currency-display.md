# 展示货币与汇率换算

管理端把不同渠道、不同币种的金额统一换算成**一个展示货币**再渲染。换算只发生在展示层：数据库里存的金额、计费快照、结算币种都不动。

## 为什么需要

渠道密钥的币种可以不一样。典型例子是 `基元律动`（tokenrhythm）：它的管理费用密钥按人民币计费，上游余额查询返回 `data.currency = CNY`、`data.costCny`、`data.availableBalanceCny`；同一渠道里没配管理费用密钥的凭据只能靠网关自己累计用量，而网关累计的是 **USD**。于是同一个渠道的两条凭据各自产出一条 `CostSummary`（`internal/logic/channel/cost_tracking.go` 的 `channelCostSummaries()` 按币种分组），列表里就并排出现了 `¥` 和 `$` 两行数字，看起来像两个不相干的渠道。

## 配置

管理端「系统设置 → 基础设置」里有三项，落库在 `system_settings` 表的 `base_settings` 键（JSON）：

| 字段 | 含义 | 取值 |
| --- | --- | --- |
| `displayCurrency` | 展示货币 | `USD`（默认）/ `CNY` |
| `exchangeRateMode` | 汇率来源 | `auto`（默认，走公开接口）/ `manual`（人工填写） |
| `manualUsdToCnyRate` | 手工汇率 | 1 USD = ? CNY，`manual` 模式才生效 |

`internal/logic/system/base.go` 的 `normalizeBaseSettings()` 对三个字段都做「非法值退回默认」，不会报错——只保存 `timeZone` 的老请求照样能过。

## 汇率服务

`internal/logic/system/currency.go`：

- 数据源固定为 `https://open.er-api.com/v6/latest/USD`，只取 `rates` 与 `time_last_update_utc`。
- 结果缓存在 Redis（键 `aiferry:system:currency-rate`，TTL 6 小时）。**同一个键同时承载 auto 与 manual 两种模式**，切换模式后要等缓存过期或手动刷新，这是有意为之：避免每次渲染都打外网。
- 站点只认 `USD`、`CNY` 两种货币（`supportedCurrencies`）。换算用的是「1 基准货币 = X 该货币」的语义，`Rates[base]` 恒为 1。
- 换算公式：`target = amount / Rates[from] * Rates[to]`。
- `mode = manual` 时用 `manualUsdToCnyRate` 现算 `Rates`，`Source` 记 `manual`；`auto` 模式取接口结果，`Source` 记 `auto`；接口不通退回默认汇率，`Source` 记 `fallback`。
- 手工汇率非法（≤ 0 或 > 100）会被 `normalizeManualUsdToCnyRate()` 换成默认值，不会让整个设置解析失败。

**没有汇率的货币不猜**：`Convert()` 遇到 `Rates` 里没有的货币代码，直接原样返回。宁可显示一种看不懂的货币，也不要把金额算错。

## 下发链路

- **管理端页面**：`GET /api/admin/system/currency-rate`（`CurrencyRateView`）返回当前展示货币、基准、汇率表、来源与更新时间。设置页保存后立刻重新拉一次并就地刷新，不用重启。
- **普通页面**：登录后 `GET /api/auth/config` 的 `currency` 字段（`CurrencyView`）一次性带回，`App.vue` 挂载时交给 `setDisplayCurrency()`。请求失败就 `setDisplayCurrency()` 无参重置回 USD。

## 前端

`frontend/src/lib/format.ts` 是唯一的格式化入口，持有 `displayCurrency` / `exchangeRates` 等模块级 ref：

- `setDisplayCurrency(config?)` —— 写入配置，`normalizeRates()` 兜底保证基准货币汇率为 1。
- `convertAmount(value, currency?)` —— 换到展示货币；币种缺失按 USD 处理；没有汇率时原样返回。
- `formatCurrencyAmount(value, currency, maxFrac)` —— 纯 `Intl.NumberFormat` 包装。
- `formatCost` / `formatPreciseCost` —— **先换算再格式化**，所以任何走这两个函数的金额自动跟随展示货币。
- `formatBalance(value)` —— 余额专用：`undefined` / `null` 返回 `'—'`，不再显示 `'未定价'`（余额没有「未定价」这个语义，未定价是成本的概念）。

`frontend/src/lib/cost.ts` 的 `mergeCostSummaries()` 把同一渠道的多条 `CostSummary` 合成一条：金额字段逐个换算到展示货币后求和，`currency` 记为展示货币；用量字段（token、次数）直接相加，它们本来就不是金额。

调用 `mergeCostSummaries()` 的地方：`ChannelListPanel.vue`（桌面与移动两处费用列）、`ChannelCredentialDrawer.vue`（凭据抽屉的汇总格与共享余额）。

其余跟随换算的视图：`ApiKeysView.vue`（额度、已用、可用）、`ProfileView.vue`（余额）、`DashboardView.vue`（成本图表，Y 轴名称与数据都换）。

## 不换算的地方

模型价格（`ModelPriceSummary.vue` 等）按**声明货币**原样展示。价格是计费单位本身，换算之后反而会和账单对不上。

## 相关测试

- `frontend/src/lib/format.test.ts` —— `convertAmount` 的换算、缺货币按 USD、无汇率不猜、`formatBalance` 的空值语义。
- `frontend/src/lib/cost.test.ts` —— `mergeCostSummaries` 的多币种合并、空输入、用量字段直加。
- `internal/logic/system/currency_test.go` —— 归属展示货币、手工汇率取值、非法汇率兜底、换算公式与不支持的货币。
