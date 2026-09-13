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
   仅供排障与兼容历史存档；回填统一写标准字段（见下）。

2. **回填（读路径）**：请求进入 `/v1/chat/completions` 时，对每条 `role=assistant`、带
   `tool_calls` 的消息规整思考内容字段，输出恒为**唯一的标准字段 `reasoning_content`**。
   客户端用 `/v1/responses` 且上游是 Chat 端点时，请求体会先经 `responses_to_chat` 转换，
   转换后（此时工具调用历史已成 `tool_calls` 形态）会再补一次，详见
   `docs/protocol-conversion.md`。

   每条消息的处理顺序：

   1. 取出客户端已带的内容（优先 `reasoning_content`，其次 `reasoning`）；
   2. **删除 `reasoning` 字段**（无论取值，见「有毒字段」）；
   3. 内容非空则以 `reasoning_content` 写回；
   4. 内容为空则用 `tool_call id` 查存档，命中就写回存档内容；
   5. 仍无内容时，若 `reasoning_content` 是 `null` 则归一成空串。

   「客户端已带」的判定不能只看字段是否存在：

   - 客户端（Codex 系）重建历史时会写出 `"reasoning_content": null`，而 `gjson` 的
     `Exists()` 对 JSON `null` 返回 `true`。若据此跳过回填，`null` 会原样透传，严格的上游
     （OpenCode Go）判定为「字段缺失」直接 400。因此只认**非空字符串**为「已带」，
     `null` 与空串都交给存档回填。
   - 查不到存档时也不能把 `null` 留给上游：归一成空串（上游接受空串、拒绝 `null`）。
   - 客户端只带了聚合方言 `reasoning` 时，内容**转写到** `reasoning_content`，同时把
     `reasoning` 删掉。

### 字段名方言与「有毒字段」

思考内容的字段名各家不一致，但实测结论非常明确：**只有 `reasoning_content` 是安全的，
`reasoning` 是有毒字段**。

捕获侧两种名字都识别（`reasoning_content` 优先，其次 `reasoning`），但回填侧**只写
`reasoning_content`**，并**删除**客户端带来的 `reasoning`。

在 OpenCode Go（`opencode.ai/zen/go/v1`）全 37 个模型上实测的字段容忍度：

| 字段取值 | GLM 系 | DeepSeek 系 | Kimi 系 |
| --- | --- | --- | --- |
| `reasoning_content: ""` | 200 | 200 | 200 |
| `reasoning_content: null` | 200 | **400** | 200 |
| `reasoning: 任意值`（含 `""` / `null`） | **400** | **400** | 200 |

结论：

- **OpenCode Go 只认 `reasoning_content`，不认 `reasoning`**：只写 `reasoning` 会被判定为
  「字段缺失」而 400，所以标准名必须写。
- `reasoning` 会**主动**让 GLM 与 DeepSeek 系 400，哪怕只是空串或 `null`。早期版本曾尝试
  「标准名 + 方言」双写以兼容两类端点，实测直接把 GLM-5.x 从 200 打成 400，已放弃。
- 因此客户端若按聚合方言 `reasoning` 重建了历史，不能只是「补齐标准字段」，必须**把
  `reasoning` 删掉**，否则会毒到严格上游。

早期存档是纯文本（无字段名），读取时按标准字段名 `reasoning_content` 兼容处理。

回填发生在路由之前，无法预知本次请求会落到哪个上游，所以「统一输出唯一标准字段」是
唯一在跨渠道轮转下都成立的方案。

两条路径都只在「模型确实产生过思考内容」时生效，对不使用思考模式的模型没有任何副作用，也不会
为无关请求增加 Redis 调用（只有发现缺少字段的工具调用消息时才查询）。

## 生效条件与限制

- 只对**上游是 `/chat/completions`** 的请求生效。上游走 `/responses` 时思考内容以 reasoning
  summary 形式传递，不在本机制覆盖范围内（客户端是 Responses 端点、上游是 Chat 端点的组合
  则在覆盖范围内，见 `docs/protocol-conversion.md`）。
- 存档依赖 Redis 与同一个 API 密钥。**Redis 重启、键超过 24 小时、或客户端切换了 API 密钥时
  命中不了存档**。此时不再抛原始 400：客户端历史里值为 `null` 的思考字段会被归一成空串，
  严格上游（OpenCode Go）接受空串，请求可以继续，只是丢失了该轮的推理上下文。
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

- 注入逻辑的单测见 `internal/logic/relay/reasoning_echo_test.go`（捕获两种字段名方言、按 tool_call
  id 回填、客户端已带标准字段时原样保留、聚合方言转写并删除、`null` 回填、无存档时 `null`
  归一成空串、空串回填、无工具调用不动请求体、非 Chat 请求体不动、存档往返与旧格式兼容）。
