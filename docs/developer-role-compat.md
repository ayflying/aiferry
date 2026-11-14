# developer 角色兼容层（过渡实现，后续应删除）

## 结论

网关在所有 `/chat/completions` 上游请求上，把消息里的 `developer` 角色改写成 `system`。
这是一层**过渡期兼容代码**，上游普遍支持 `developer` 之后应当整体删除，不要保留开关或空实现。

## 为什么需要

`developer` 是 OpenAI 后来引入的消息角色，语义上等价于 `system`，用来承载优先级更高的指令。
新版客户端（DSH、Codex 等）已经改用 `developer` 代替 `system`，但国内多数上游的
`/chat/completions` 兼容层仍只接受 `system` / `user` / `assistant` / `tool` 四种角色，
遇到 `developer` 直接判为参数错误。

生产实证（2026-11-14 22:23，`afreq_8d9dc997518d6e75f21dc5cf`）：

- 渠道 26（智谱），模型 `glm-5.3-flash`，HTTP 400，耗时 180ms（未走到模型）；
- 响应体：`{"code":"1214","message":"角色信息不正确"}`；
- 报文首条消息为 `{"role":"developer",...}`，同一份请求体打其它渠道正常。

即：问题出在上游是否认识这个角色名，与模型能力、网关转发链路无关。
`1214` 是智谱自己的错误码，网关只是原样透传。

## 怎么实现

`internal/logic/relay/developer_role.go` 的 `normalizeDeveloperRole`，在
`prepareRequestBody` 里对 `/chat/completions` 端点执行：

- 只改 `role` 字段：`developer` → `system`；
- `content`（含多模态 parts 数组）、`tool_calls` 等其余字段原样保留；
- 非 `/chat/completions` 端点不处理（Responses 的 `input` 由协议转换负责）；
- `messages` 结构不符合预期时不做任何猜测，直接跳过。

为什么是全局而不是按渠道开关：这是上游兼容性缺口，不是某家渠道的特性。按渠道开关意味着每接入
一家不认 `developer` 的上游都要补一次配置，且新渠道默认踩坑；全局归一让所有渠道对新客户端
一致可用，符合网关「抹平上游差异」的职责。

与 workbuddy 的关系：`internal/logic/channel` 的 `ApplyWorkBuddyBody` 另有一套
`developer` → `system` 归一，那是该渠道风控适配的一部分。本层先执行，改写成 `system` 后
workbuddy 的归一逻辑看到的已经是 `system`，行为不变，两者互不影响。

## 何时删除

一旦国内主流上游（智谱、Kimi、通义、豆包等）的 `/chat/completions` 都接受 `developer`，
本层就是多余的历史包袱，应整体删除：

1. 删除 `prepareRequestBody` 里的 `normalizeDeveloperRole(endpoint, payload)` 调用；
2. 删除 `internal/logic/relay/developer_role.go` 与 `developer_role_test.go`；
3. 删除本文档。

判断标准：拿首条消息为 `{"role":"developer"}` 的请求直打各上游，全部返回 200 即可删。

## 验证

单测见 `internal/logic/relay/developer_role_test.go`，覆盖：`developer` 改写且内容不变、
原有 `system` 不受影响、多模态 parts 原样保留、无 system 时不额外插入消息、
Responses 端点不受影响、无 `developer` 时请求体不变、非法结构安全跳过、改写计数准确。
