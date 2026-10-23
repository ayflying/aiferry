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

此次为解决已复现的系统提示触发 11128，未移植参考项目的不可见字符扰动、账号池、凭据读取、客户端伪装、失败后的强制提示改写。流式发送沿用 AiFerry 现有逻辑，避免引入第二套实现。未额外移植 tool_choice 和模型 reasoning 档位策略，因为它们不属于此次已复现问题。

该接口不是稳定的公开 API，上游限制可能变化；此适配不保证账号或接口持续可用。生产修改必须单独授权，单元测试通过不等于已部署。

## 跟随更新

1. 对比固定提交之后的 `src/upstream.ts` 及 `tests/issue98-channel-gate.spec.ts`、`tests/issue113-channel-gate-evidence.spec.ts`。
2. 优先同步上述函数行为；如需扩大移植范围，明确记录新增差异与风险，不直接覆盖 Go 文件。
3. 更新 Go 回归用例、本文固定提交及第三方声明。
4. 本机验证后，用授权账号做原失败请求体回放；仅接收响应，不执行回放产生的工具调用。记录实际渠道、请求 ID、HTTP 与有效生成结果，不保存密钥。
