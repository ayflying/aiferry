# 价格同步与上游计费表达式

## 数据源

公共价格源（`price_sources` 表）内置 BaseLLM 官方价：

| 字段 | 值 |
| --- | --- |
| code | `basellm_official` |
| baseUrl | `https://basellm.github.io` |
| pricing.adapter | `newapi_ratio` |
| pricing.path | `/llm-metadata/api/newapi/ratio_config-v1-base.json` |

同步手动触发（管理端「模型价格」→ 选择价格源 →「同步模型价格」→ `POST /api/admin/prices/sync`），没有定时任务。

## 上游格式变更（2026-09）

BaseLLM 把倍率配置换成 new-api 计费表达式：`data.billing_expr`（model → expr）+ `data.billing_mode = tiered_expr`，
并删除了 `model_ratio` / `completion_ratio` / `cache_ratio` / `model_price`。
旧字段缺失时同步会报 `NewAPI ratio source did not return model_ratio`。

表达式是 expr-lang 语法，系数即 USD/1M tokens 实价，结构为「时段 → 思考模式 → 上下文长度」三层三元，
叶子统一是 `tier("<档名>", <线性表达式>)`。解析实现按职责拆在同包三个文件里：

| 文件 | 职责 |
| --- | --- |
| `internal/logic/channel/price_expr.go` | 入口 `syncedRulesFromBillingExpressions`、三元递归展开、规则组装 |
| `internal/logic/channel/price_expr_rates.go` | 叶子与线性表达式 → 费率字段 |
| `internal/logic/channel/price_expr_condition.go` | 时段 / 长度 / 请求参数条件解析 |

旧倍率字段仍兼容（`syncedRulesFromNewAPIRatio` 先认 `billing_expr`，没有才走旧逻辑）。

## 模型名匹配

上游 key 与本地公开名有四种常见差异，`price_name.go` 按匹配强度从高到低逐级尝试，命中即停：

| 强度 | 场景 | 例子 |
| --- | --- | --- |
| 精确 | 与本地名称逐字符相同 | `glm-5.3` |
| 大小写 | 整体忽略大小写同名 | `OpenAI/GPT-4O` → `gpt-4o` |
| 后缀 | 最后一个 `/` 之后的名称同名 | `z-ai/glm-5.3-flash` → `glm-5.3-flash` |
| 变体 | 去掉厂商前缀与 `:peft:` / `:sampling-` 后缀后同名 | `zai-org/GLM-5.3:peft:262144` → `glm-5.3` |

同一个公开模型被多条上游条目命中时只保留强度最高的一组（`ruleBucket`），因此上游同时给出标准名与
微调变体名时，变体的高价不会覆盖标准价；只有上游没有标准名条目时才用变体价兜底。
匹配先按公开名、再按上游名（别名）两级进行，第二级只补公开名没有直接来源的缺口。

## 变量映射

| 表达式变量 | 费率字段 |
| --- | --- |
| `p` | `inputPerMillion` |
| `c` | `outputPerMillion` |
| `cr` | `cachedInputPerMillion` |
| `cc` | `cacheWritePerMillion` |
| `cc1h` / `cc12h` | 仅当没有 `cc` 时按 1h → 12h 顺序回退到 `cacheWritePerMillion` |
| `ai` / `ao` | `audioInputPerMillion` / `audioOutputPerMillion` |
| `fixed(x)` | `request`（带 `* image_count` 时同样是 `request`，金额为每张图单价） |

表达式未引用 `cr` 时不写 `cachedInputPerMillion`，计费端会把缓存 token 回退按输入价计算，
与 new-api「未引用该变量就不从 p 中剔除」的语义一致。

**所有系数为 0 的项一律丢弃**：上游用 `p * 0` 表达「该维度不计费」，若按 0 落库，模型会被判成
已定价却永远按 0 计费。整条表达式系数全为 0（订阅制、免费额度、企业独占）时判为「无价格」，
返回 `errBillingExprNoPrice` 并计入跳过统计，绝不落成 0 价规则。

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

## 跳过与零价防护

同步结果除写入条数外还返回跳过明细（`PriceSyncResult.skipped` / `skippedCount`）。跳过不是失败：

| 原因 | 处理 |
| --- | --- |
| 表达式系数全为 0 | 判为未按量计费，跳过并清空该模型历史同步规则 |
| `billing_mode` 不是 `tiered_expr` | 跳过并提示暂不支持 |
| 表达式无法解析 | 跳过并记录错误原文，提示上游格式可能变更 |

跳过会同时清理该公开名的历史同步规则，并把公共价格里**值为 0 的字段清成 NULL**：
`0` 在计费上是「已定价且免费」，`NULL` 才是「没有价格」，两者对 `IsPriced`（余额预检）与
`EstimateBreakdown` 的影响完全相反。若只删规则不清 0 价，旧数据会让模型继续按 0 计费。

写入阶段还有第二道零价防护 `splitZeroPricedRules`：任何价格源（含普通 JSON 列表）给出全 0 费率都
不会落库。公共价格合并时同样把「当前值为 0」当作未定价，允许后续同步用真实价格覆盖。

## 当前限制

1. **峰谷档中嵌套其他条件时只保证覆盖**：`weekday/hour` 的 else 是补集，若该分支下还有 `len`
   分档，时段外会落到兜底档的对应长度档；高峰时段若长度落在仅兜底档声明的区间，仍按非高峰价计费
   （偏低）。当前 base 数据里峰谷模型都是「时段 → 金额」两档，不触发这条。
2. **思考模式**：`qwen` 系 thinking 档无法按请求参数路由，统一按「未开启档」计价（偏低）。
3. **按图计费按次落库**：`tier("image", fixed(x)) * image_count` 是每张图 x 美元。图像生成端点
   不返回 usage，折算成图像输入 tokens 会让规则永远算不出金额（请求以「用量不可计费」失败），
   因此落成 `request` 费率。已知粒度损失：`n > 1` 的一次请求仍按一份计价，与上游 `model_price`
   的既有口径一致。
4. **CNY 渠道价差**：上游单价是 USD 口径，CNY 计价的厂商渠道（如智谱）同步后仍是 USD 数，
   不按渠道币种换算，展示与计费都以同步到的 USD 价为准。
