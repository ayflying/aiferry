# Responses ↔ Chat Completions 协议转换

## 什么时候会转换

客户端端点与上游端点不一致时，网关自动转换请求与响应，客户端不需要感知：

| 客户端请求 | 上游端点 | 转换方向 |
| --- | --- | --- |
| `/v1/chat/completions` | `/responses` | `chat_to_responses`（GPT 系模型默认走这条，让 Chat 客户端用上模型的原生 Responses 实现） |
| `/v1/responses` | `/chat/completions` | `responses_to_chat`（Responses 客户端访问只提供 Chat 能力的上游，如 `opencode_go` 这类聚合渠道） |

只有在「首选方案失败且错误表明端点不被支持」时才会回退到直通（见 `protocol.ShouldFallback`）。
失败日志里的「协议转换：」行会写明实际使用的转换方向。

## 开关：关闭协议转换

转换由 `internal/logic/relay/protocol_attempt.go` 的 `attempt` 按两级开关决定：

| 级别 | 位置 | 语义 |
| --- | --- | --- |
| 全局 | 系统设置 → 协议转换（`protocolConversionEnabled`，存 `system_settings` 的 `channel_resilience`） | 默认启用；关闭后所有渠道直连客户端声明的端点 |
| 渠道 | 渠道 → 高级配置 → 协议转换（`advanced_config.protocolConversion`） | 三态：`null` 跟随系统、`true` 强制启用、`false` 强制关闭 |

渠道级优先于全局。关闭时 `preferredProtocolPlan` 直接返回 `protocol.DirectPlan`，
且 `attempt` **不再回退到转换端点**——`AlternatePlan` 给出的备选端点必然要求转换，
继续回退等于绕过开关。因此若某上游只支持 Responses 端点，关闭后它的 Chat 请求会直接
收到该端点的原始错误，而不是被默默转成 Chat 再重试。

用途是排查「转换引入的问题」。典型场景：客户端报工具调用参数序列化异常
（多个并行调用的参数被拼成一个 JSON、工具名对不上）。关掉开关后用同一 prompt 复测，
若恢复正常即可确认问题出在转换环节；若仍异常，则问题在上游或客户端。

读取失败一律按未声明处理（fail-open）：系统设置缺字段时默认 `true`，渠道类型配置
读取异常时回退按模型名推断，都不阻断转发。

## 两种协议的形态差异

同一段对话，两种协议的表示方式完全不同：

| 语义 | Chat Completions | Responses |
| --- | --- | --- |
| 助手文本 | `assistant` 消息的 `content` | `message` 输入项（`content` 为 `output_text` 数组） |
| 工具调用 | 挂在 `assistant` 消息的 `tool_calls` 数组 | 独立的 `function_call` 输入项（`call_id` + `name` + `arguments`） |
| 工具结果 | `role=tool` 消息（`tool_call_id`） | 独立的 `function_call_output` 输入项（`call_id`） |
| 思考内容 | `reasoning_content` / `reasoning` 字段 | 独立的 `reasoning` 输入项（`summary` / `content`） |

Chat 侧「一次工具调用」是 assistant 消息的一部分，Responses 侧则被拆成平级的多个输入项，
因此转换时要解决**拆分与合并**两个方向的问题。

## 实现要点

转换代码集中在 `internal/logic/protocol/`：`request.go`（请求，`responsesInputToChat` /
`chatToolCallsToResponses`）、`response.go`、`stream.go`、`protocol.go`（计划与入口）。

`responsesInputToChat` 的规则：

1. **`function_call` → assistant 的 `tool_calls`**。`call_id` 是与 `function_call_output`
   对应的键（`id` 仅在客户端只填了 `id` 时兜底），两边的 id 必须一一对应，否则上游会报
   「工具结果找不到对应的工具调用」。
2. **合并**。相邻的 `function_call`（并行调用）合成一条 assistant 消息的多个 `tool_calls`；
   紧邻其前的 assistant 文本消息也一并合并，还原成 Chat 里「content + tool_calls」的单条消息。
3. **`reasoning` → `reasoning_content`**。同一段里出现的 `reasoning` 输入项，其 `summary` /
   `content` 文本挂到合并出来的那条 assistant 消息上。只在存在工具调用时携带（没有工具调用
   时该字段不参与上游校验，丢弃可避免上游拒绝未知字段）。
4. **未知输入项直接跳过**。`local_shell_call`、`computer_call` 等在 Chat 侧没有对应形态，
   跳过它们；早期实现会把它们降级成 `{"role":"user","content":null}`，等于往对话里注入一条空
   用户消息，破坏对话结构。

### 流式响应的并行工具调用

`responsesToChat` 把 Responses 的平级 `function_call` 输出项还原成 Chat 的
`tool_calls[]` 增量，两个约束必须同时满足，否则客户端会把并行调用读错：

1. **下标从 0 起连续分配**，不能照搬上游 `output_index`。Responses 的 `output_index`
   会把 message、reasoning 等输出项一起计入，透传会在客户端留下空洞。
2. **`item.id` 与 `call_id` 作为别名指向同一个下标**。`response.output_item.added`
   同时带 `item.id` 与 `call_id`，而部分上游的 `response.function_call_arguments.delta`
   只带 `item_id`、不带 `call_id`。只认单一键会把同一次调用的「名称」与「参数」算成
   两次调用、落到两个下标上：客户端看到的是多个工具的参数被拼进一个 JSON
   （`Unexpected non-whitespace character after JSON`），且工具名对不上
   （`Tool not found in agent cli`）。

工具名每个调用只发送一次：上游会重复下发 `output_item.added`，重复发送会让客户端把
`function.name` 按增量拼成 `PowerShellPowerShell`。

### 转换后再补一次思考内容

`reasoning_content` 回传（见 `docs/reasoning-echo.md`）在请求进入时按 Chat 消息结构补写，
但 Responses 客户端的历史里没有 `messages`，思考内容是以 `reasoning` 输入项表达的。因此
`attemptWithProtocol` 在 **`responses_to_chat` 转换完成之后**再对转换出来的 Chat 请求体调用一次
回填：`call_id` 与存档使用的 `tool_call id` 是同一个值，能正常命中。

## 验证

```bash
go test ./internal/logic/protocol/...
```

覆盖用例：`TestResponsesRequestToChat`、`TestResponsesToolContinuationToChatRestoresToolCalls`、
`TestResponsesUnknownInputItemDoesNotBecomeUserMessage`、`TestChatToolContinuationToResponsesIncludesFunctionCallItems`
（反向转换）、`TestResponsesStreamDoesNotRepeatToolName`、`TestResponsesStreamKeepsParallelToolIndexes`、
`TestResponsesStreamMatchesToolArgumentsByItemID`（delta 只带 item_id 时的下标一致性）。

开关相关：`TestProtocolConversionDisabledPinsClientEndpoint`（relay 侧）、
`TestParseAdvancedConfigProtocolConversionThreeStates`（渠道配置三态解析）。
