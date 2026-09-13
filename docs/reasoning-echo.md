# thinking 模式模型的 reasoning_content 回传

## 现象

多步工具调用（agent 循环）到第二、第三轮时，上游返回 HTTP 400：

```text
Error from provider (Console Go): Upstream request failed: [invalid_request_error]
The `reasoning_content` in the thinking mode must be passed back to the API.
```

或 Moonshot/Kimi 的等价提示：

```text
thinking is enabled but reasoning_content is missing in assistant tool call message at index 2
```

## 原因

DeepSeek、Kimi 等 thinking 模式的上游是**无状态**的：只要本次对话发生过工具调用，中间那条
assistant 消息的 `reasoning_content` 就必须在后续每一轮**原样回传**，否则上游拒绝请求。

`reasoning_content` 不是 OpenAI Chat Completions 的标准字段，绝大多数兼容客户端重建历史消息时
只保留 `role` / `content` / `tool_calls`，会把它丢掉。网关原样透传也无法补救——请求体里本来
就没有这个字段。

该约束**与渠道类型无关，只取决于具体模型是否开启思考模式**。例如 `opencode_go` 这类聚合渠道
同时挂了 37 个模型（deepseek / kimi / glm / qwen / minimax / grok / gpt 等），其中 deepseek、
kimi 需要回传，grok、gpt 不需要。因此实现里既不做渠道类型判断，也不维护模型名白名单。

## 实现

转发层承担两件事，都在 `internal/logic/relay/reasoning_echo.go`：

1. **存档（写路径）**：转发时从上游响应里累积思考内容与它绑定的工具调用 id。流式从
   `choices[0].delta.reasoning_content`（或聚合渠道的 `delta.reasoning`）与
   `delta.tool_calls[].id` 累积；非流式读 `choices[0].message`。按 `tool_call id` 写入 Redis：

   ```text
   aiferry:reasoning:<API 密钥 ID>:<tool_call id>  ->  {"field":"reasoning_content","text":"..."}
   ```

   TTL 24 小时，按 API 密钥作用域隔离，避免不同用户之间串号。存档连**字段名**一起保存，
   回传时沿用上游自己的方言（见下）。

2. **回填（读路径）**：请求进入 `/v1/chat/completions` 时，对每条 `role=assistant`、带
   `tool_calls`、且尚无思考内容字段的消息，用它的 `tool_call id` 查存档；命中则按存档记录的
   字段名补回真实内容。客户端已经带了该字段时不覆盖，查不到存档时保持原样交给上游处理。
   客户端用 `/v1/responses` 且上游是 Chat 端点时，请求体会先经 `responses_to_chat` 转换，
   转换后（此时工具调用历史已成 `tool_calls` 形态）会再补一次，详见
   `docs/protocol-conversion.md`。

### 字段名方言

思考内容的字段名各家不一致，且同一个聚合渠道里也可能混用：

| 上游 | 字段名 |
| --- | --- |
| DeepSeek / Kimi / GLM / MiMo 直连 | `reasoning_content` |
| OpenCode 系聚合端点（含 `opencode_go`） | `reasoning` |

因此实现**不写死字段名**：捕获时记录实际命中的字段名（优先 `reasoning_content`，其次
`reasoning`），回传时按同一个名字写回。否则会出现「上游用 `reasoning` 返回、网关按
`reasoning_content` 补回」，上游读不到补回的内容，仍然 400。

早期存档是纯文本（无字段名），读取时按标准字段名 `reasoning_content` 兼容处理。

两条路径都只在「模型确实产生过思考内容」时生效，对不使用思考模式的模型没有任何副作用，也不会
为无关请求增加 Redis 调用（只有发现缺少字段的工具调用消息时才查询）。

## 生效条件与限制

- 只对**上游是 `/chat/completions`** 的请求生效。上游走 `/responses` 时思考内容以 reasoning
  summary 形式传递，不在本机制覆盖范围内（客户端是 Responses 端点、上游是 Chat 端点的组合
  则在覆盖范围内，见 `docs/protocol-conversion.md`）。
- 存档依赖 Redis 与同一个 API 密钥。**Redis 重启、键超过 24 小时、或客户端切换了 API 密钥时
  命中不了存档**，该轮仍会拿到上游的原始 400（此时需要重新发起一次对话）。
- 流式响应被客户端中断（未收到 `[DONE]`）时不存档，避免下一轮回传出残缺的推理。
- 存档值超过 1 MiB 时跳过写入并打 WARN 日志，避免异常上游写出超大键值。

## 排障

- 失败日志里出现「协议转换：」行时，说明请求经过了 Responses/Chat 协议转换：方向是
  `responses_to_chat`（工具调用被还原成 `tool_calls`）时本机制仍然生效；方向是
  `chat_to_responses`（上游才是 Responses 端点）时不生效。
- 确认存档是否写入：

  ```bash
  redis-cli --scan --pattern 'aiferry:reasoning:*'
  ```

- 注入逻辑的单测见 `internal/logic/relay/reasoning_echo_test.go`（捕获、两种字段名方言、
  按方言回传、去重、已有字段不覆盖、无工具调用不动请求体、查不到存档保持原样、存档往返与
  旧格式兼容）。
