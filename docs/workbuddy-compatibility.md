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

此次为解决已复现的系统提示触发 11128。2026-10-23 用真实失败报文在生产上游做了三组对照实测：原始请求 400、只做前缀中性化仍 400、前缀中性化加扰动 200。这说明触发点不是消息开头，而是系统提示内部的整段精确子串（与参考实现 issue #98 的实测结论一致）。因此追加移植 `perturbChannelFingerprints`：仅在上游以 `11128` 拒绝后对同一请求重发一次，在 system/assistant 文本的句读边界插入 U+200B 零宽空格，打断整段精确匹配；语义不变、对用户不可见，每个请求最多一次。未移植参考项目的账号池、凭据读取、官方客户端身份伪装、失败后的强制提示改写、tool_choice 与 reasoning 档位策略。

该扰动会让上游看到的文本字节与客户端原文不同（可见内容与语义不变）。这是通过第三方客户端渠道校验的手段，上游明确不欢迎非官方客户端，可能随时加强检测或处罚账号；风险由使用该渠道的账号承担。

该接口不是稳定的公开 API，上游限制可能变化；此适配不保证账号或接口持续可用。生产修改必须单独授权，单元测试通过不等于已部署。

## 跟随更新

1. 对比固定提交之后的 `src/upstream.ts` 及 `tests/issue98-channel-gate.spec.ts`、`tests/issue113-channel-gate-evidence.spec.ts`。
2. 优先同步上述函数行为；如需扩大移植范围，明确记录新增差异与风险，不直接覆盖 Go 文件。
3. 更新 Go 回归用例、本文固定提交及第三方声明。
4. 本机验证后，用授权账号做原失败请求体回放；仅接收响应，不执行回放产生的工具调用。记录实际渠道、请求 ID、HTTP 与有效生成结果，不保存密钥。
