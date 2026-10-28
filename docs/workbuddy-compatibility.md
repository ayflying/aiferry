# WorkBuddy 消息兼容

## 来源

参考 [dsh-connect-workbuddy](https://github.com/dingminhua/dsh-connect-workbuddy)，固定提交 `4cdba8a37b277a465b754328737f580254552d34`，源文件 [src/upstream.ts](https://github.com/dingminhua/dsh-connect-workbuddy/blob/4cdba8a37b277a465b754328737f580254552d34/src/upstream.ts)。MIT 版权与许可全文见 [第三方声明](../THIRD_PARTY_NOTICES.md)。Go 移植和 AiFerry 接线由本项目负责，参考来源不代表原作者保证本项目兼容性。

## 适用范围与行为

仅 `workbuddy` 渠道、上游 chat completions 请求启用；Responses 等协议先由现有转换层转换为 chat，再执行本处理。其他渠道不变。

移植上游 `prepareChatBody` 的 developer → system、`ensureSystemHead`、`neutralizeChannelFingerprints` 与前缀判定：

- 将 developer 角色规范化为 system。
- 缺少 system 首条时添加 `You are a helpful assistant.`。
- 对 system/assistant 消息中命中已知身份前缀的内容添加同一中性开场；大小写和前导空白匹配方式与参考一致。
- 字符串和数组中的每一段 text 均覆盖。原始内容完整保留，不删除规则，不改 user/tool 消息，不改工具参数和图片。
- 已兼容的请求保持字节不变；重复执行不叠加开场。

此次为解决已复现的系统提示触发 11128。2026-10-23 用真实失败报文在生产上游做了三组对照实测：原始请求 400、只做前缀中性化仍 400、前缀中性化加扰动 200。这说明触发点不是消息开头，而是系统提示内部的整段精确子串（与参考实现 issue #98 的实测结论一致）。因此追加移植 `perturbChannelFingerprints`：在上游以 `11128` 拒绝后对同一请求重发一次，在 system/assistant 文本的句读边界插入 U+200B 零宽空格，打断整段精确匹配；语义不变、对用户不可见。未移植参考项目的账号池、凭据读取、官方客户端身份伪装、失败后的强制提示改写、tool_choice 与 reasoning 档位策略。

### 0.6.25：首跳即扰动，重试降档（偏离参考实现）

2026-10-26 生产实证（payload `afreq_50304d2f…`，DB `usage_logs.attempt_flow_json`）：带指纹的对话在 16:00:50 连续 10 次上游调用全被 11128 拒绝——原始体返回 "Illegal API invocation from an unapproved channel"，扰动体返回 "request illegal"；同一份字节 16:04:55 起单发重放（跨 0.6.20 与 0.6.24 两版本）连续 11 次通过。结论：上游在账号吃下指纹拒绝后会进入分钟级的「拦截窗口」，窗口内连扰动体一并拒绝；「失败后扰动重试」的第一发原始体本身就在触发并延长这个窗口，客户端重发继续喂窗口，形成用户侧永远失败的死循环，而低频单发的重放永远落在窗口外。

因此本版本偏离参考实现的「失败后扰动」时序：

- 首跳即扰动：workbuddy chat 请求在 `ApplyWorkBuddyBody` 之后立刻执行 `PerturbWorkBuddyBody`，第一次上游调用就是扰动体，不再用注定失败的原始体去触发窗口；扰动幂等，已含零宽空格的文本不叠加。
- 移除 attempt 内部的「11128 后扰动重发一次」：首跳已是扰动体，重发同一字节在窗口内必然重演失败，只会放大对风控的刺激。
- 风控退避降档：最后候选的 11128 退避从 1.5s/5s/15s/30s（4 次、累计 ~52s）降为 2s/8s（2 次）。秒级短窗口仍可自愈；分钟级窗口由「模型 × 密钥」组合冷却兜底，用户稍后重发在窗口外首跳即通过。

### 0.6.26：身份从句逐词打断（补齐 5.11.0 的 IDENTITY_CLAUSE）

0.6.25 部署后生产实证（afreq_862d819d，2026-10-26 18:21）：三发扰动体仍全部 11128 “request illegal”。取回失败报文比对，定位到 DSH 系统提示新增的身份从句 “You are a coding agent powered by the <model> model.”——没有相邻大写词对、没有句中逗号、终结句点落在指纹之外，0.6.25 稀疏扰动的全部规则对它零命中；这与参考仓库 5.11.0 记录的三处实测指纹（issue #125、workbuddy2api-hub #533 逐字节二分）一致。移植其 IDENTITY_CLAUSE 方案：大小写不敏感匹配 “you are … agent powered by …” 与 “you are interacting with the user through the deepseek harness” 两类从句，在从句内部每个空白后插入 U+200B——上游指纹是子串匹配且无从得知登记的是从句哪一段，逐词打断使任何端点的指纹都无法完整命中；模型名可变，匹配到 “powered by” 为止，可变尾部由从句内打断兜底。与参考实现一致，该打断随首跳扰动对每个请求执行（实测约万字符 +28 字节）。

两个移植差异需要记录：其一，从句打断必须先于稀疏扰动执行——稀疏扰动先在 “DeepSeek Harness” 等词间插零宽空格会让从句正则的空白匹配失败，上游可能登记的前缀指纹反而保持完整；其二，稀疏扰动不再以“整段含零宽空格即跳过”保证幂等，改为边界级幂等（CJK 句号要求后继不是零宽空格；专有名词改为整链匹配后链内逐空白打断，避免相邻匹配共享同一个词导致漏插）。扰动仍只作用于 system/assistant 文本，user/tool 消息与工具参数保持原样。

### 0.6.27：补齐官方客户端身份头

0.6.27 前的最后一次生产失败（afreq_965cb90f，2026-10-27 10:52）给出决定性证据：同一条报文由本网关低频重放返回 200，而用户的真实请求 3 次尝试全部 11128 "request illegal"；失败体 `displayTips` 原文写着「这次调用没通过渠道校验，可能不是官方客户端发出的」。这说明内容扰动层已经放行，剩下的门槛是「请求看起来不像官方客户端」。

因此补齐官方客户端的身份头（对应参考实现 issue #66 / #132）：官方 CLI 的 User-Agent（`CLI/2.63.2 CodeBuddy/2.63.2`）、`Origin`/`Referer`（`https://www.codebuddy.cn`）、`X-Requested-With`、`X-Product: SaaS`，以及 conversation 身份三连 `X-Conversation-ID` / `-Request-ID` / `-Message-ID`（32 位 UUIDv7，每请求重新生成，`X-Request-ID` 与 message id 同值）。参考实现逐个二分官方 `applyGatewayHeaders` 后确认只有这三件必需，`acp-connection-id`、`X-Agent-Intent`、`X-IDE-*`、`X-Private-Data` 与 trace 头都不是门槛；网关侧未知用户身份时按官方 CLI 的写法发 `X-No-User-Id` 一类约定头。

客户端自报的第三方会话标识（`X-Session-Id`、`X-Agent-Intent`、`X-IDE-*` 等）在补身份前清除：上游同时看到官方三连与第三方标识的混合身份比单纯缺失更容易被判定为非官方。清除必须在写入官方身份之前执行——`http.Header` 会规范化头名，顺序反了会把刚写入的官方值一并删掉。

该层与文本扰动层相互独立：扰动让正文不再命中指纹，身份头让请求在客户端维度上像官方客户端；两者都保留。

### 0.6.28：渠道特化文件拆分与自动签到

**代码组织**。按「每个渠道的特殊处理放在以该渠道命名的文件里」重排：`upstream_client_headers.go` 现在只做分派（`applyChannelClientHeaders`）与通用逻辑（会话派生、通用 UA 覆盖），各渠道实现分别落在 `workbuddy_identity.go`、`opencode_free.go`、新增的 `opencode_go.go`。新增渠道特化只需在分派处加一行，实现进该渠道自己的文件，不与其它渠道的分支互相覆盖。

**自动签到**。渠道额度查询原本只读签到状态（`checkin-activity-status`，需官方客户端 Turing SDK 生成的 `X-Device-Token`，服务端无法复现，故一直是尽力而为）。签到领取走另一条路径 `POST {billingBase}/v2/billing/meter/daily-checkin`，请求体 `{}`，只需 `Authorization: Bearer`、`Accept`、`Content-Type` 三个头，服务端可以正常复现——因此签到本身可以自动完成。

额度查询会为渠道下**每个上游密钥（一个密钥即一个账号）**并发执行：先读积分，再读签到状态；读到明确未签到时调用领取接口，结果以「已自动签到，获得 N 积分，连续 M 天」写进额度提示。状态读不到（缺风控头）时不盲目领取——无法判断今天是否已签，重复调用没有意义；状态字段缺失或解析失败则按未签到处理，因为上游重复签到是幂等的（返回 `code=0`、`credit=0`），代价远小于漏签一天。

**额度弹窗展示全部账号的合并结果**。密钥抽屉里的额度入口与渠道列表的额度按钮走同一个渠道级查询（`GET /channels/{id}/quota`，不带 `credentialId`），不再是「一次只看一个账号」。积分窗口按账号累加（总量、已用、剩余各自相加），打开就是整条渠道的总盘子。

**签到显示为计数口径**。签到窗口表达的是「今天 N/M 个账号完成签到」，不是单账号的百分比。合并时按计数累加、再用 `已完成/总数` 重算比例——若沿用「百分比取平均」，两个账号一个签了一个没签会显示成 50% 进度条，看着像额度用了一半，实际和签到进度毫无关系。同理签到窗口不占用 `used/total/remaining` 这组额度字段，避免被读成一笔余额。

**不再误报「需要管理密钥」**。渠道类型在 `builtins.json` 里省略 `costs` 段时，`DefaultConfig()` 会兜底成 OpenAI 风格的费用查询（`openai_costs` + `management_key`，请求 `/organization/costs`），WorkBuddy 并没有这个接口，于是管理端点「查询费用」必然以「channel type requires a management key」失败——这个报错和真实原因无关，容易误导排查方向。现在 WorkBuddy 显式声明 `"costs": { "adapter": "none" }`，界面上只保留积分/额度入口，费用查询入口不再出现。

**后台兜底签到**。额度查询只在用户打开渠道额度详情时触发，若整天不打开管理端就会漏签，因此另起一个后台任务（`workbuddy_checkin_job.go`）：**每天 6:00 执行一次**，遍历所有 WorkBuddy 渠道下的每个账号，先读状态、明确未签才领取。用户没打开管理端也能自动签上；两个入口共用同一套请求与解析逻辑（`claimWorkBuddyCheckin`），不会出现两处实现漂移。

签到是自然日一次的动作，因此调度用每天固定时刻（`KindDailyAt`）而不是按小时轮询——2026-10-28 用户指出每小时检查没有意义。服务启动 90 秒后若已过 6 点会补跑一轮，这一轮用于发版/重启当天不漏签，且同样先读当天状态、已签到的账号直接跳过，不会重复领取。

该扰动会让上游看到的文本字节与客户端原文不同（可见内容与语义不变）。这是通过第三方客户端渠道校验的手段，上游明确不欢迎非官方客户端，可能随时加强检测或处罚账号；风险由使用该渠道的账号承担。

该接口不是稳定的公开 API，上游限制可能变化；此适配不保证账号或接口持续可用。生产修改必须单独授权，单元测试通过不等于已部署。

### 0.6.42：积分查询改走 `/v2` API 网关

**现象**。管理端「查询积分余额」长期返回 `HTTP 401，密钥无效、已过期或没有余额查询权限`，而同一把密钥的自动签到一直正常。

**根因**。积分走的是桌面端网页路由 `POST /billing/meter/get-user-resource-summary`（host 根），该地址在 openresty 的网页会话门之后：服务端带渠道密钥请求只拿到 `401 Authorization Required` 的 **HTML** 页，请求根本没进业务逻辑。用无效密钥逐条探测可区分「路由不存在（404）」与「路由存在但缺鉴权（401）」，结果是 `/v2/billing/meter/get-user-resource` 存在且与签到接口同族，而 `/v2/.../get-user-resource-summary` 为 404。

**改法**。积分请求路径改为 `/v2/billing/meter/get-user-resource`，与签到一起走接受渠道密钥的 API 网关；积分明细解析改成字段名无关的递归扫描（识别 `CycleTotalCapacity` / `total_capacity` / `TotalCredit` 等别名，去重后累加），避免 `/v2` 路由的外壳与旧网页接口不一致时再次失效。查询成功与解析失败都会打日志（端点、窗口数、已用/余额、原始响应片段），生产上可直接看到真实结构。

### 0.6.44：积分余额确认只对网页登录会话开放

**现象**。0.6.42 改走 `/v2` 后，生产日志里积分查询依旧每次都是 `status=401` + openresty 的 HTML 页；同一把密钥的签到与用量查询正常。用户反馈「网页上能查到，早期版本也能查」。

**复核过程**。用登录态浏览器抓包看到决定性的两条事实：网页端真正调用的余额同族接口 `POST https://www.workbuddy.cn/billing/meter/checkin-status` 返回 200，但该请求带 `cookie` 且**完全没有 Authorization 头**；而无效密钥探测显示 `/v2` 下不存在的路由返回 `404 text/plain`，`/v2/billing/meter/get-user-resource` 返回 HTML 401——路由存在，但拒绝渠道密钥。0.6.44 又追加了「Origin/Referer 与积分站点同源 + 浏览器 UA + 清除 chat 身份头」的请求形态优先尝试，两种形态在生产上仍都是 HTML 401，因此**跨站 Origin 不是原因**，会话才是门槛。

**结论**。`/billing/*` 与网页共用 Cookie 会话；渠道 accessToken 能查签到与用量，但拿不到这份会话，服务端因此读不到积分余额。这不是可以靠改请求头修掉的回归。

**改法**。请求形态回退保留（同源浏览器形态优先，chat 身份形态兜底），成功会打 `workbuddy credits query ok: variant=...`；失败文案不再猜测“风控限流”，而是如实说明会话边界并指向官网查看余额。签到与用量链路不受影响。

## 跟随更新

1. 对比固定提交之后的 `src/upstream.ts` 及 `tests/issue98-channel-gate.spec.ts`、`tests/issue113-channel-gate-evidence.spec.ts`。
2. 优先同步上述函数行为；如需扩大移植范围，明确记录新增差异与风险，不直接覆盖 Go 文件。
3. 更新 Go 回归用例、本文固定提交及第三方声明。
4. 本机验证后，用授权账号做原失败请求体回放；仅接收响应，不执行回放产生的工具调用。记录实际渠道、请求 ID、HTTP 与有效生成结果，不保存密钥。
