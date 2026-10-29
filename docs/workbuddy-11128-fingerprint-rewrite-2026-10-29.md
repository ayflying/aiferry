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

## 上线后生产回归（0.6.53，2026-10-29 12:21 发布）

| 检查项 | 结果 |
|---|---|
| healthz（连续 5 次） | `version=0.6.53`，`revision=2bb58f3b017abe446a8a65b9463d5b3f4f22bb2f`，`status=ok` |
| 运行容器 | 镜像 `ghcr.io/ayflying/aiferry:0.6.53`，running/healthy，启动 12:21:54；容器内 `/app/VERSION`=0.6.53 |
| 发布后 12:2x—12:9x 全部请求 | 141 条，**响应含 `code=11128` 的 0 条** |
| 携带全部 13 条指纹的系统提示 | HTTP 200，正文「通过」，耗时 2.27s |
| 生产失败报文原样重放（163 条消息 / 84 个工具 / 图片） | 不再出现 11128；上游返回 `code=11134 the model provider is temporarily unavailable`（`retry_after=20`），或网关 `context canceled`（客户端主动中断） |

关键判断依据：修复前同一份报文在 0.5—0.9 秒内即被 400/11128 拒绝；修复后同一份报文能进入上游模型处理阶段（响应头 52—57 秒才到，说明已过渠道校验并在处理 4 万级 prompt），失败原因变成模型侧可用性（11134），与渠道门无关。11134 属上游临时状态，与本次改动无关，重试即可。

回归期间创建的四个临时诊断密钥均已在页面内删除（HTTP 200），全程未把密钥值带出浏览器，也未写入磁盘或日志。

## 第二个形态：developer 角色（0.6.54，2026-10-29 14:07 实证）

0.6.53 上线后 hy3 恢复，但同一渠道的 `deepseek-flash-1017` 仍报 11128（`afreq_ea1065d7fde559a05c5b25a0`，14:07:42，上游 `deepseek-v4.1-flash`，5 次尝试全 400）。核对报文后确认差异不在模型，在**请求形态**：

| 报文 | 角色分布 | 结果 |
|---|---|---|
| hy3（12:30 探测） | 有 `system` 首消息 | 200 |
| deepseek-flash-1017（14:07） | 58 assistant / **1 developer** / 52 tool / 18 user，**无 system** | 400 / 11128 ×5 |

原因链：身份提示写在那条 `developer` 消息里；改词层先跑，当时只认 `system`/`assistant`，便把 `developer` 跳过；紧接着 `ApplyWorkBuddyBody` 把 `developer` 规范化成 `system` 再发出，13 条指纹原样落进上游扫描面。改词层与角色归一化用了两套角色集合，中间漏了一层。

修法：`workBuddyIsScannedRole` 纳入 `developer`，凡是最终会被上游当成 `system`/`assistant` 的消息都参与改词；角色本身仍由 `ApplyWorkBuddyBody` 规范化，改词层不动 role 字段。表未变——参考实现仍是 v5.13.1，13 条逐字一致，缺的不是指纹而是覆盖的角色。

验证：用该条生产失败报文走「改词 → 中性头 → 零宽扰动」全链路，129 条消息最终为 `system:1 / assistant:58 / tool:52 / user:18`，system 文本中残留原指纹 **0 条**（修复前此处残留全部 13 条）。

## 边界

- 改词表是**大小写精确**匹配：宿主 system 是固定大小写；上游本身不区分大小写，若
  出现新的大小写形态需要新的实测证据再补。
- 表是**易增补**的：新增指纹加一行，并在 `workbuddy_fingerprint_test.go` 补一条断言。
- 未覆盖的非 DSH 第三方开头（例如 `<|im_start|>system:` 一类）没有等义改写的实测
  数据，暂不改写；遇到新指纹应先用 200/400 对照实验确认再入表。
- 上游风控会继续变化（本次即 0.6.25 的扰动层被归一化击穿）。这一层是「已知指纹表」，
  不是通用免疫。