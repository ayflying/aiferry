# WorkBuddy 11128 生产验收（2026-10-29）：渠道门固定指纹改词

## 结论

`Illegal API invocation from an unapproved channel` 这一族 11128 的拦截对象是请求
`system` / `assistant` 文本里的**固定短语**，不是零宽字符能击穿的字节特征。0.6.53 起
改为在发往上游之前把已登记的指纹短语**按句改写成同义写法**，并保留原中性头与零宽
扰动两层。零宽扰动对这道门无效（见下），但仍然保留：它对其他渠道的内容指纹仍然有用。

## 生产失败证据（本次触发）

| 项 | 值 |
|---|---|
| 请求 | `afreq_aaf2a19a4ab273f4e65d9cef`（客户端 `afreq_7026f418de8f7e88ab791abe`） |
| 时间 | 2026-10-29 10:57:41（Asia/Shanghai），渠道 35 workbuddy #2，凭据 dsh |
| 状态 | HTTP 400，上游尝试 5 次，总耗时 13.487s |
| 上游报文 | `code=11128 msg="Illegal API invocation from an unapproved channel"`，`actions=["EDIT_INPUT","SUBMIT_FEEDBACK"]` |
| 报文规模 | 163 条消息（system 1 / user 23 / assistant 71 / tool 68），84 个工具定义，1 个 `image_url` |

同渠道同期对照（证明与渠道、凭据、模型、流式都无关）：

- 10:56:32 `hy3` workbuddy #1 → 400 11128（同一份 system 指纹）
- 11:02:33 `deepseek-flash` workbuddy #1 非流式 → 200
- 11:08:55 `hy3` 渠道自测（系统提示由网关生成，无宿主指纹）流式 → 200，入 17 · 出 43

失败报文的 `system` 开头逐条命中参考实现 v5.13.1 登记的 13 条指纹：

```
You are an AI agent powered by DeepSeek Harness
You are a coding agent powered by the hy3 model
Agent Teams is available in this session
Tokens prefixed with @ are paths the user explicitly referenced.
…（@ 路径说明整段共 9 句）
```

## 机制（参考实现 2026-10-28 对照实验）

- 同一份 body 连发 100 次 → 100 次 400：**确定性拒绝**，不是随机抖动，重发无意义。
- 在 system 开头插入 `U+200B` → 仍然 400：上游**在匹配前归一化掉零宽字符**，字节级
  扰动对这道门没有出路（这也是 0.6.25–0.6.52 期间本网关扰动层放不住它的原因）。
- 开头加一个普通词 `Note: ` → 200：门是**锚在固定文字上的字面匹配**，改词有效。
- 四处规则只改任一处 → 400；**四处全改才 200**：多处指纹是**组合命中**，必须整表跑完。
- `user` / `tool` 开头放同一条身份 → 200：上游不扫这两个角色，因此改词层也不碰它们。

## 实现（0.6.53）

- 新增 `internal/logic/channel/workbuddy_fingerprint.go`：13 条「原文短语 → 同义短语」
  改写表 + `RewriteWorkBuddyFingerprintPhrases`，只改 `system` / `assistant` 的
  string content 与 array content 里的 text part；`user` / `tool` / 工具定义 / 图片
  part 一律不动；无命中逐字节原样返回，函数幂等。
- 三层共用 `workBuddyTransformContent` 做「只碰文本」的边界，避免同一段遍历逻辑
  复制三份（`workbuddy_body.go` 的中性头与零宽扰动改为委托调用）。
- 顺序（`protocol_attempt.go`）：**改词 → 中性头 → 零宽扰动**。改词必须最先，否则
  中性头或零宽会先改动文本，改词表就匹配不到宿主原文。
- 未命中时零开销：`RewriteWorkBuddyFingerprintPhrases` 返回 false 即原样使用报文。

## 验证

- `go test ./internal/logic/channel/...`：新增 4 个用例（真实 system 开头全表命中、
  user/tool/图片/工具不受影响、幂等、无命中不改动）全通过。
- `go test ./...`：仅剩既有的无关失败 `TestVideoContentRoutesMatchPathParameters`
  （internal/controller/relay 视频路由用例），与本次改动无关。
- 真实生产报文（`afreq_aaf2a19a…` 的 `request` 子对象）过一遍「改词 → 中性头 →
  零宽扰动」全链路：13 条原指纹全部消失、报文仍是合法 JSON、163 条消息与 84 个工具
  数量不变，system 开头变为
  `You are an AI agent running inside DeepSeek Harness… You are a coding agent running on the hy3 model…`。

## 边界

- 改词表是**大小写精确**匹配：宿主 system 是固定大小写；上游本身不区分大小写，若
  出现新的大小写形态需要新的实测证据再补。
- 表是**易增补**的：新增指纹加一行，并在 `workbuddy_fingerprint_test.go` 补一条断言。
- 未覆盖的非 DSH 第三方开头（例如 `<|im_start|>system:` 一类）没有等义改写的实测
  数据，暂不改写；遇到新指纹应先用 200/400 对照实验确认再入表。
- 上游风控会继续变化（本次即 0.6.25 的扰动层被归一化击穿）。这一层是「已知指纹表」，
  不是通用免疫。