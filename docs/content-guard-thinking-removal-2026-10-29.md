# 删除 deepseek 思考块改写（2026-10-29）

## 一句话结论

网关原来有一层「思考块改写」：上游把思考用 `<thinking>…</thinking>` / `<analysis>…</analysis>`
写进正文时，把它搬进 `reasoning_content` 并剥掉标签。该层是按 adesk 渠道 deepseek 系列的实证
加的模型专用补丁，用户 2026-10-29 确认上游已修复标签错乱，**这套代码已整体删除**；
同层里与模型无关的「协议标记拒答」保留。

## 处理过程

1. 先按用户要求把守卫调用整体注释（0.6.55，commit `58319ba`）做排除法实验；
2. 实验确认思考可见性正常后，确认问题不在网关改写这一层；
3. 0.6.56 按用户指令删除思考块改写实现，恢复协议标记拒答。

## 删除的内容（deepseek 专用的一半）

| 文件 | 内容 |
| --- | --- |
| `content_guard_thinking.go`（整文件） | `leakOpeners`、`leakCloser`、`<analysis>` 边界、`splitLeakedThinking`、`stripLeakOpener` 等思考块识别与切分 |
| `content_guard_probe.go`（整文件） | 思考块探测状态机（`guardThinking` 分支、思考块未收起时的暂存与重新判定） |
| `content_guard_hold.go`（整文件） | `holdThinking` / `releaseHeld`（暂存思考保住重试窗口）、`hasPendingDraft`、`Finish` 收尾拒答、`guardStreamTerminal` |
| `content_guard_tools.go`（整文件） | `releaseToolCall`（把正文草稿搬进思考后再交付工具调用） |
| `content_guard_analysis_test.go`、`content_guard_terminal_test.go`、`content_guard_tools_test.go` | 上述行为的测试 |

`content_guard.go`、`content_guard_buffered.go`、`protocol_attempt.go` 相应瘦身：
状态机只剩 `idle / probing / done`，非流式只剩「正文整体以协议标记开头即判坏响应」。

## 保留的内容（与模型无关的一半）

上游把自家客户端 / Agent 框架的内部标记当正文写出时仍判为上游坏响应、不交付客户端：

- `<tool_result`、`<tool_call(s)>`、`<tool invoke>`、`｜…｜` DSML 收尾标记（workbuddy 渠道实证）；
- `<summary>…</summary>` 摘要信封（adesk 实证 `afreq_04b72bca01aaab19292942ab`）；
- 刻意不拦 `<compacted-summary>`，避免打断客户端自己的上下文压缩。

保留原因：这类内容无法还原成正确的工具调用，客户端解析失败会把垃圾存进会话历史，下一轮
再原样回传，最终表现为「对话在没有回答的情况下中断」（2026-10-27 生产实证）。它与具体模型
无关，删掉等于放弃这条护栏。

## 行为变化（需要知悉）

- 上游若仍把 `<thinking>` / `<analysis>` 写进正文，网关**按普通正文原样透传**：不搬进
  `reasoning_content`、不剥标签、不因它拒答；
- 「只写出思考草稿、没有给出回答」的 502 收尾判定随之取消，这种响应照常交付；
- 唯一保留的拦截：正文**开头**就是客户端内部标记时仍判 502（不写出任何字节，保住候选重试窗口）。

## 若上游标签错乱复现怎么办

从 git 历史取回 `58319ba` 之前的 `content_guard_thinking.go` 等四个文件及对应测试，
恢复 `streamContentGuard` 的 `held/tail/closer` 字段与 `Observe` 中的思考分支即可。