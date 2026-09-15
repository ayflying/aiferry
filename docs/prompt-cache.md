# 渠道级提示缓存字段处置

## 背景

请求体里的 `prompt_cache_key`、`prompt_cache_options`、`prompt_cache_retention`，以及消息
内容块里的 `prompt_cache_breakpoint`，合称提示缓存控制字段。网关默认接管它们：先剥离
客户端带来的值，再按「用户 + 公开模型 + 渠道 + 凭据」生成一个稳定缓存键。这样同一用户的
连续请求命中同一缓存桶，而共用同一把上游密钥的多个用户不会互相命中彼此的前缀缓存。

## 三种模式

渠道高级配置项 `promptCacheMode`：

| 取值 | 行为 | 适用场景 |
| --- | --- | --- |
| 缺省 / `stable` | 剥离客户端缓存字段，注入 `aiferry:<sha256 前 16 字节>`，身份串为 `v1\|u:<用户 ID>\|m:<公开模型>\|c:<渠道 ID>\|k:<凭据 ID>` | 默认，需要用户级缓存隔离的渠道 |
| `off` | 剥离客户端缓存字段，且**不下发任何缓存字段** | 上游对请求字段做白名单校验（返回 `UNKNOWN_FIELD`） |
| `passthrough` | 完全不干预，缓存字段由客户端自行控制 | 客户端自己管理缓存 |

历史配置里的 `passthroughPromptCache: true` 等价于 `passthrough`，且优先于 `promptCacheMode`
生效。前端的三态下拉选「跟随客户端」时会把这两个字段一起写回，因此老配置的语义不变。

判定顺序在 `channel.AdvancedConfig.ResolvePromptCacheMode()`；`promptCacheMode` 出现未知取值
时解析直接报错（fail-closed），不会静默回落到某个模式。

## 为什么需要 off

2026-09-15 基元律动（`tokenrhythm.studio`，渠道类型 `tokenrhythm`）在当天上午收紧请求字段
校验，不认 `prompt_cache_key` 的服务端一律返回：

```
error: code="UNKNOWN_FIELD" message="未知请求字段：prompt_cache_key" data={"field":"prompt_cache_key"}
```

而该渠道 9/14 的 152 次正式请求全部 200 —— **上游行为变了，网关侧必须能做到「一个缓存
字段都不发」**：`stable` 会主动注入，`passthrough` 又会把客户端自带的字段原样放过，两者都
救不了。同类历史：ManyTokens（ch23）在 8/30–8/31 报过 16 次完全相同的错误。

## 模型测试与正式转发共用同一实现

`channel.ApplyPromptCachePolicy` 同时被转发链路（`relay.applyPromptCachePolicy`）和模型测试
链路（`channel.buildTestRequest`）调用。此前测试请求体是独立构造的、根本不含缓存字段，
因此这类问题**只能靠正式流量暴露** —— 表现为「点测试通过、正式请求 400」。现在两条链路
按同一策略处置：渠道声明 `off` 时测试同样不下发缓存字段，声明缺省时测试同样注入稳定键。

## 排查线索

- `usage_logs.error_message` 同时出现 `UNKNOWN_FIELD` 与 `prompt_cache_key`；
- 同一分钟内 `test:/chat/completions` 返回 200、而 `/chat/completions` 返回 400 ——
  这个对照基本可以直接判定是「测试链路与转发链路的请求体不一致」，而不是渠道本身故障；
- 核查该渠道 `advanced_config` 的 `promptCacheMode`：默认（缺省/`stable`）是主动注入的一方。
