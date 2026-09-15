# 流式失败切换与截断收尾

流式请求「突然停下来、对话中断、也没有报错」的成因与现有边界，以及截断后的判定规则。

## 一、候选切换的覆盖范围

`Handle` 的候选循环（`internal/logic/relay/service.go`）依次尝试 `route()` 返回的全部候选。
候选来源是「按公开模型名匹配到的所有 `channel_models` 行」（`enabled=1` 且未被自动禁用），
因此**同一渠道把多个上游模型映射成同一个公开名，就是多个独立候选**，会逐个尝试。

候选内的重试维度（不消耗跨渠道预算）：

- 同一渠道的不同上游密钥（`attemptChannel` 循环换密钥，`SelectCredential` 排除已试过的）
- 渠道的备用 BaseURL（`candidateBaseURLs`）
- 协议端点回退 `/chat/completions` ↔ `/responses`（`protocol_attempt.go`，受协议转换开关约束）

全部候选耗尽时，controller 回写明确的 JSON 错误（503 + `Retry-After`、429 等）。

## 二、分水岭：首个可见 token

`protocol_attempt.go` 用 `committed` 控制缓冲：**只有还没有可见输出时**，SSE 行才进 `pending`
缓冲（上限 1MiB）。第一个可见 delta 一到，立刻 flush 并 `committed = true`，之后逐行直写客户端。

这意味着**切换能力只存在于「首个可见 token 之前」**。过了这条线就不能再重放，只能就地收尾。

因此下列「无内容落地」的失败会正常切换下一个候选：

| 失败 | 处理 |
| --- | --- |
| 连接/首字节超时、传输错误 | `wroteBytes=false` → 交回上层切换 |
| 上游 4xx/5xx | 同上，并受重试状态码规则约束 |
| 流内 error 事件且尚未写出内容 | `return result, false, nil` → 切换 |
| 流式响应没有任何内容也没等到结束标记 | `attemptCompleted` 返回 false → 切换 |

## 三、已写字节后的三种收场

历史上这三条都是**静默断流**：客户端只看到连接结束，网关也不认为请求失败。

1. **上游流内 error 事件**（`response.failed`、`{"error":{"code":402,...}}`）：
   `parseStreamFailure` 命中但 `wroteBytes` 为真 → 无法切换，就地收尾。
2. **空闲超时或上游半路断连**：`scanner.Err()` 只写 `errorMessage`，随后走到函数末尾收尾。
3. **干净 EOF 但没有 `[DONE]` / `response.completed`**：`scanner.Err()` 为 nil，
   历史上 `status` 仍是 200，会被当成成功。

0.5.120 起：

- 三条路径都按「**已写出内容且未收到结束标记**」判定为截断（`streamTruncated`）。
- 网关补发显式终止帧（见第四节），客户端据此提示「上游中断」，而不是静默停下。
- 截断不再计入成功：不写健康加分、不清零失败计数，改走与其它上游失败相同的失败通道
  （`maybeAutoDisable`，是否真的禁用仍由 `disableStatusCodes` / 关键词 / 超时规则决定）。
- 用量记录按 **502** 落库并写明原因，**不扣费**。

### 客户端主动断开不算渠道故障

下游客户端取消对话或关闭页面时，写入会失败、请求上下文会被取消。这种情况标记为
`writerFailed` / `ctx.Err() != nil`，**不参与渠道与模型的失败评分**，避免把用户行为算成上游故障。

## 四、终止帧的形态

按客户端所在协议生成（`streamTerminalLines`，取 `plan.ClientEndpoint()`）：

- Chat Completions 客户端：`data: {"error":{"message":"...","type":"upstream_error","code":502}}`
  后跟一个空行，再补 `data: [DONE]` 收尾。
- Responses 客户端：`event: error` 事件行 + `data: {"type":"error","error":{...}}`，
  以空行结束该事件。

终止帧会经过敏感数据还原器，与正常响应走同一条回写路径。

## 五、配置开关

管理端「系统设置 → 熔断」的**流式失败提示**（`streamFailureEventEnabled`，默认开启）。

- 开启：补发错误事件 + 结束标记，客户端能明确提示上游中断。
- 关闭：保持直接断开流的历史行为，适合依赖「断流即重试」的客户端（如 Codex）。

**开关只影响是否补发终止帧**；截断一律不再算成功、一律不计费，与开关无关。

字段缺省（历史数据）按开启处理 —— `decodeSettings` 先取 `DefaultResilienceSettings()`
再 unmarshal，缺失的键会保留默认值。

## 六、排障指引

看用量详情里的 `attempt_flow`：

- **只有一步、状态 200、无错误文本**：历史上属于第三节第 3 种（截断被记成成功）。
  0.5.120 起应显示 502 + 截断原因。
- **有多步、含 502/402**：切换确实发生过，失败发生在首个 token 之前。
- **一步、状态 200、有截断原因**：上游在给出内容后中断，属于第三节第 1/2 种。

关联文件：

- `internal/logic/relay/protocol_attempt.go` —— 流式转发与分水岭
- `internal/logic/relay/stream_failure.go` —— 上游失败解析与客户端终止帧
- `internal/logic/relay/credential_retry.go` —— `attemptCompleted` 的候选内/跨候选判定
- `internal/logic/relay/usage.go` —— 截断的落库与计费