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
   `choices[0].delta.reasoning_content` 与 `delta.tool_calls[].id` 累积；非流式读
   `choices[0].message`。按 `tool_call id` 写入 Redis：

   ```text
   aiferry:reasoning:<API 密钥 ID>:<tool_call id>  ->  reasoning_content
   ```

   TTL 24 小时，按 API 密钥作用域隔离，避免不同用户之间串号。

2. **回填（读路径）**：请求进入 `/v1/chat/completions` 时，对每条 `role=assistant`、带
   `tool_calls`、且缺少 `reasoning_content` 的消息，用它的 `tool_call id` 查存档；命中则补回
   真实内容。客户端已经带了该字段时不覆盖，查不到存档时保持原样交给上游处理。

两条路径都只在「模型确实产生过思考内容」时生效，对不使用思考模式的模型没有任何副作用，也不会
为无关请求增加 Redis 调用（只有发现缺少字段的工具调用消息时才查询）。

## 生效条件与限制

- 只对**上游是 `/chat/completions`** 的请求生效。上游走 `/responses` 时思考内容以 reasoning
  summary 形式传递，不在本机制覆盖范围内。
- 存档依赖 Redis 与同一个 API 密钥。**Redis 重启、键超过 24 小时、或客户端切换了 API 密钥时
  命中不了存档**，该轮仍会拿到上游的原始 400（此时需要重新发起一次对话）。
- 流式响应被客户端中断（未收到 `[DONE]`）时不存档，避免下一轮回传出残缺的推理。
- 存档值超过 1 MiB 时跳过写入并打 WARN 日志，避免异常上游写出超大键值。

## 排障

- 失败日志里出现「协议转换：」行，说明请求经过了 Responses/Chat 协议转换；本机制不覆盖该路径。
- 确认存档是否写入：

  ```bash
  redis-cli --scan --pattern 'aiferry:reasoning:*'
  ```

- 注入逻辑的单测见 `internal/logic/relay/reasoning_echo_test.go`（捕获、去重、已有字段不覆盖、
  无工具调用不动请求体、查不到存档保持原样）。
