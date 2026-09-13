# Responses ↔ Chat Completions 协议转换

## 什么时候会转换

客户端端点与上游端点不一致时，网关自动转换请求与响应，客户端不需要感知：

| 客户端请求 | 上游端点 | 转换方向 |
| --- | --- | --- |
| `/v1/chat/completions` | `/responses` | `chat_to_responses`（GPT 系模型默认走这条，让 Chat 客户端用上模型的原生 Responses 实现） |
| `/v1/responses` | `/chat/completions` | `responses_to_chat`（Responses 客户端访问只提供 Chat 能力的上游，如 `opencode_go` 这类聚合渠道） |

只有在「首选方案失败且错误表明端点不被支持」时才会回退到直通（见 `protocol.ShouldFallback`）。
失败日志里的「协议转换：」行会写明实际使用的转换方向。

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
（反向转换）、`TestResponsesStreamDoesNotRepeatToolName`、`TestResponsesStreamKeepsParallelToolIndexes`。
