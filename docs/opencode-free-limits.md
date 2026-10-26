# OpenCode Zen 免费渠道限制

## 2026-10-26 排查

用户收到 HTTP 429，错误类型为 `FreeUsageLimitError`，文案为：

> Free usage limit reached. Free models work in OpenCode; subscribe to OpenCode Go to keep going.

这不是已确认的客户端版本过旧或协议不兼容错误，不能将更新 UA、刷新会话或切换 IP 当作修复方法。仅凭这条错误，也无法判断限额按账号、IP、模型还是其他维度计算，或何时恢复。

官方当前文档明确规定：免费聊天模型仅能在 OpenCode 内使用；其他客户端应使用付费模型或 Go 订阅。即使提供 API key，也不代表获得第三方客户端免费调用权限。

- [官方推理 API 与鉴权规则](https://opencode.ai/v2/docs/console/api/inference/)
- [官方模型清单](https://opencode.ai/v2/docs/console/models/)

## 网关处理

- 对 HTTP 429 的结构化 `FreeUsageLimitError` 不再执行最后一个候选的 1.5 秒同候选退避重试。
- 普通 429 瞬时限流的既有重试行为不变。
- 既有密钥选择、跨渠道兜底和健康评分规则不变；未找到其他可用渠道时保留上游错误，不伪报恢复成功。
- 不因免费额度耗尽禁用整把密钥，避免误伤其仍有权限的付费模型。
- 不轮换身份、会话或 IP 绕过免费使用限制。

## 运维边界

如需在第三方网关继续使用 OpenCode，应配置有授权的付费模型或 Go 渠道；涉及新增订阅、计费或生产路由调整，需要单独确认。本次本机回归测试只能证明重试分类修复正确，不能证明生产免费渠道恢复。
