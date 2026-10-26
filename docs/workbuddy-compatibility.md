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

该扰动会让上游看到的文本字节与客户端原文不同（可见内容与语义不变）。这是通过第三方客户端渠道校验的手段，上游明确不欢迎非官方客户端，可能随时加强检测或处罚账号；风险由使用该渠道的账号承担。

该接口不是稳定的公开 API，上游限制可能变化；此适配不保证账号或接口持续可用。生产修改必须单独授权，单元测试通过不等于已部署。

## 跟随更新

1. 对比固定提交之后的 `src/upstream.ts` 及 `tests/issue98-channel-gate.spec.ts`、`tests/issue113-channel-gate-evidence.spec.ts`。
2. 优先同步上述函数行为；如需扩大移植范围，明确记录新增差异与风险，不直接覆盖 Go 文件。
3. 更新 Go 回归用例、本文固定提交及第三方声明。
4. 本机验证后，用授权账号做原失败请求体回放；仅接收响应，不执行回放产生的工具调用。记录实际渠道、请求 ID、HTTP 与有效生成结果，不保存密钥。
