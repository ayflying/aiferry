# 价格同步与上游计费表达式

## 数据源

公共价格源（`price_sources` 表）内置 BaseLLM 官方价：

| 字段 | 值 |
| --- | --- |
| code | `basellm_official` |
| baseUrl | `https://basellm.github.io` |
| pricing.adapter | `newapi_ratio` |
| pricing.path | `/llm-metadata/api/newapi/ratio_config-v1-base.json` |

同步手动触发（管理端「价格同步」→ `POST /api/admin/price-sync`），没有定时任务。

## 上游格式变更（2026-09）

BaseLLM 把倍率配置换成 new-api 计费表达式：`data.billing_expr`（model → expr）+ `data.billing_mode = tiered_expr`，
并删除了 `model_ratio` / `completion_ratio` / `cache_ratio` / `model_price`。
旧字段缺失时同步会报 `NewAPI ratio source did not return model_ratio`。

表达式是 expr-lang 语法，系数即 USD/1M tokens 实价，结构为「时段 → 思考模式 → 上下文长度」三层三元，
叶子统一是 `tier("<档名>", <线性表达式>)`。解析实现见 `internal/logic/channel/price_expr.go`；
旧倍率字段仍兼容（`syncedRulesFromNewAPIRatio` 先认 `billing_expr`，没有才走旧逻辑）。

## 变量映射

| 表达式变量 | 费率字段 |
| --- | --- |
| `p` | `inputPerMillion` |
| `c` | `outputPerMillion` |
| `cr` | `cachedInputPerMillion` |
| `cc` | `cacheWritePerMillion` |
| `cc1h` | 仅当没有 `cc` 时回退到 `cacheWritePerMillion` |
| `ai` / `ao` | `audioInputPerMillion` / `audioOutputPerMillion` |
| `fixed(x)` | `request` |

表达式未引用 `cr` 时不写 `cachedInputPerMillion`，计费端会把缓存 token 回退按输入价计算，
与 new-api「未引用该变量就不从 p 中剔除」的语义一致。

## 条件映射

| 上游条件 | AiFerry 规则条件 |
| --- | --- |
| `len <= N` | `inputTokensAtMost: N`，补集规则 `inputTokensAtLeast: N + 1`；多级链逐层求交 |
| `weekday()` / `hour()` 峰谷 | 命中分支写 `time: {tz, weekdays, ranges}`（时区沿用上游声明的 `UTC`）；else 分支是补集，无法用「与」条件表达，落成不带时段条件的兜底规则 |
| `param("enable_thinking")` | 无法表达请求参数条件，取 else（未开启档）并在规则名标注 |

带条件的模型会自动切到 `billing_mode = rules`（`setPublicBillingMode`），否则规则入库也不参与计费；
纯无条件模型维持原计费模式，只更新公共价格字段。规则入库顺序为「无条件先、条件后」，
匹配按 id 降序逐条试错，条件规则因此先于兜底规则命中。

峰谷档的展开顺序同样依赖这条约定：`orderSyncedRules` 先展开 else（非高峰兜底）、再展开命中分支
（高峰、带时段条件），于是兜底规则 id 更小、后试，高峰时段先命中高峰价，时段外落到兜底价。
若把高峰档写成不带时段条件，高价会覆盖全天；若让兜底规则先于高峰档命中，则高峰价永不生效。

单个模型解析失败只跳过该模型，不阻断整源同步；全部失败才报错，用于提示上游格式再次变化。

## 当前限制

1. **峰谷档中嵌套其他条件时只保证覆盖**：`weekday/hour` 的 else 是补集，若该分支下还有 `len`
   分档，时段外会落到兜底档的对应长度档；高峰时段若长度落在仅兜底档声明的区间，仍按非高峰价计费
   （偏低）。当前 base 数据里峰谷模型都是「时段 → 金额」两档，不触发这条。
2. **思考模式**：`qwen` 系 thinking 档无法按请求参数路由，统一按「未开启档」计价（偏低）。
3. **按图计费**：`tier("image", fixed(x)) * image_count` 这类表达式无法解析，该模型会被跳过（base 数据暂无此形态）。
