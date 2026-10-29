# 临时屏蔽「正文污染守卫」（2026-10-29，排查 deepseek 思考不可见）

## 一句话结论

为排查「deepseek 的思考过程看不到了」，把网关上唯一会改写「客户端实际收到的正文与思考」的
一层特殊处理——**正文污染守卫（content guard）**——的调用临时注释掉，让上游响应原样透传。
守卫实现一行未删，恢复只需取消注释。

## 为什么只屏蔽守卫

按 `deepseek|DeepSeek` 在生产代码里逐处核对，网关**没有按模型名特判的分支**；与思考相关的
处理只有三处，其中只有守卫会改变交付给客户端的内容：

| 处理 | 位置 | 是否改变客户端可见内容 | 本次处置 |
| --- | --- | --- | --- |
| 正文污染守卫（流式状态机 + 非流式整包判定） | `internal/logic/relay/content_guard*.go` | 会：把 `<thinking>` 等思考块搬进 `reasoning_content`、把 `<tool_result`/`<summary>` 等协议标记判为坏响应不交付 | **已注释调用（流式 + 非流式）** |
| 思考内容捕获（`reasoningCapture` / `captureBufferedReasoning`） | `internal/logic/relay/reasoning_echo.go` | 不会：只把上游思考聚合成报文记录，不回填请求、不改写响应 | 保留（注释说明其性质） |
| `reasoningToContent`（把思考包 ` thinking` 写进正文） | `internal/logic/channel/advanced.go` | 不会：早已下线，`ParseAdvancedConfig` 只做兼容删除 | 保留（本来就不生效） |

另外 `normalizeResponseBody` / `normalizeSSELine` 只补 `model`/`created`/`object` 元数据，
对正文、思考与工具字段一律原样保留（见 `internal/logic/relay/transform.go:144`）。

## 改动内容

`internal/logic/relay/protocol_attempt.go`（注释掉调用，不删除实现）：

1. 非流式：`inspectBufferedContent(...)` 调用与 `markupLeak` 坏响应分支整段注释；
2. 流式：`contentGuard := newStreamContentGuard(...)` 构造注释；
3. 流式循环内 `contentGuard.Observe(...)` 判定块与「协议标记泄漏」拒答分支整段注释；
4. 流式收尾 `contentGuard.Finish()` 的「只写出思考草稿」拒答分支整段注释。

`internal/logic/relay/content_guard_attempt_test.go`：`TestContentGuardAttemptRetainsRetryWindow`
临时 `t.Skip`（该用例断言「草稿被拦、客户端零字节、保留重试窗口」，守卫停用后前提不成立）。
守卫实现本身未改动，`content_guard_test.go`、`content_guard_analysis_test.go`、
`content_guard_tools_test.go` 仍直接调用 `newStreamContentGuard` 覆盖同一套判定，全部通过。

## 屏蔽期间的行为差异（预期，需知悉）

- 上游把思考写进正文（`<thinking>…</thinking>`、`<analysis>…</analysis>`）时，**正文里会带着
  标签原样到达客户端**，不再被搬进 `reasoning_content`；若思考块之后紧跟 `<summary>` 摘要
  信封，也会照常交付；
- 上游把工具协议标记（`<tool_result)`、`<tool_calls>`、DSML 等）写进正文时，网关**不再拦成
  502**，垃圾会进入客户端会话历史，可能重新引发「对话在没有回答的情况下中断」（2026-10-27
  生产实证 `afreq_04b72bca01aaab19292942ab`）；
- 不再出现「上游只写出思考草稿、没有回答」的 502 收尾判定。

## 已核实的现象（本轮只读取证，未改代码）

- 15:25–15:26 的 23 条 deepseek 请求里，17 条上游返回了非空思考（最长 1066 字节），3 条字段
  为空串、3 条无该字段；
- 思考为空的那些响应，正文开头都是正常中文，**不是** `<` 标签形态，说明守卫当时并未介入；
- 因此本次屏蔽是「排除法实验」：若屏蔽后思考仍不可见，问题在上游或客户端侧，不在网关改写。

## 恢复步骤

1. 取消 `internal/logic/relay/protocol_attempt.go` 中四段注释（含 `contentGuard` 构造）；
2. 删除 `internal/logic/relay/content_guard_attempt_test.go` 里的 `t.Skip` 一行；
3. `go build ./... && go test ./internal/logic/relay/...` 通过后按发布流程发版。