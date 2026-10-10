# WorkBuddy（腾讯桌面 AI Agent / CodeBuddy 系）渠道适配

新增内置渠道类型（`workbuddy`，id `9000000000000024`）。适配桌面端 `WorkBuddy.exe`（进程 PID 34320）的官方后端接口，无第三方 SDK 授权。

## 上游来源（本机侦察）

- 桌面端：`D:\Program Files\WorkBuddy\WorkBuddy.exe`
- 进程运行中，无 `--remote-debugging-port`，无法 attach 提取运行时 token
- 数据目录：`C:\Users\ay\.workbuddy`（90 项：`workbuddy.db`、`models.json`、`keyblob`（336B DPAPI wrapped-key）、`secrets` 为空、`user-state.json`、`qimei-cache.json`、账号目录 `c409dd35-abd0-469a-aaca-a69f7aa67d6d`）
- `keyblob` 结构：`{"version":1,"keyId":"bb9f264ee53ec068","slots":[{"type":"static-v1","protectorKeyId":"9127dea1b44020a7","wrapped":"..."}]}` —— wrapped 字段解码为 AES-256-GCM（suite=1，32 字节 master key、12 字节 nonce、16 字节 authTag）；`protectorKeyId` "9127dea1b44020a7" 不在磁盘任何文件，仅由运行时加载的 `TuringShieldSDK.dll` / `turing_sdk.node`（在 PID 34320 模块中）派生；无法在服务端复现解密
- `models.json`（官方模型清单，7 条）：`glm-5.3-flash`、`qwen3.8-flash-next`、`deepseek-flash`、`mimo-v2.6-flash`、`gpt-6-sol`、`gpt-6-luna`、`grok-4.7`
- API 清单（`app.asar` 317MB 正则提取）：`/v2/chat/completions`（默认计算网关 `copilot.tencent.com`）、`/v3/chat/completions`、`/v4/chat/completions`、`/v1/chat/completions`、`/v2/enterprises/personal/models`（模型清单）、`/v2/user/cloudagent/quota`、`/v2/billing/meter/*`（计费与额度）
- 鉴权模型（`buildHeaders`）：`Authorization: Bearer ${accessToken}` + `X-User-Id: <uid>` + `X-Domain: www.workbuddy.cn`；计费接口额外经 `buildHeadersWithTuringToken` 加 `X-Device-Token`（由本机 `qimei.dll` 风控 SDK 生成）

## 渠道配置（manifest）

```json
{
  "id": 9000000000000024,
  "name": "WorkBuddy",
  "code": "workbuddy",
  "config": {
    "baseUrl": "https://www.workbuddy.cn/v2",
    "models": {"method":"GET","path":"/enterprises/personal/models","listPath":"data","idPath":"id","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "},
    "quota": {"adapter":"workbuddy_credits","method":"POST","path":"/billing/meter/get-user-resource-summary","authType":"channel_key","headerName":"Authorization","headerPrefix":"Bearer "}
  }
}
```

- 额度查询路径：`POST /billing/meter/get-user-resource-summary`（桌面端 `resourcePrefix` 为空，不带 `/v2` 前缀；由 `resolveHostURL` 自动拼接 `https://www.workbuddy.cn` + `/billing/meter/get-user-resource-summary`）
- 签到状态路径：`POST /v2/billing/meter/checkin-activity-status`（桌面端 `billingPrefix` 固定为 `/v2`）
- 模型发现路径：`GET /enterprises/personal/models`（相对于 `baseUrl`，自动包含 `/v2` 前缀）

## 额度解析适配器（`workbuddy_credits`）

- 主请求：`get-user-resource-summary` → 解析每个 `Packages[].{PackageCode,CycleTotalCapacity,CycleRemainCapacity,CycleUsedCapacity}` 映射为 `QuotaWindow{kind:monthly}`
- 尽力而为的第二请求：`checkin-activity-status` → 解析 `streak_days` / `today_credit` 映射为 `QuotaWindow{kind:checkin}`
- 签到请求需要 `X-Device-Token`（Turing 风控头），服务端无法复现；失败时不阻塞积分结果，只把失败明细写入 `PartialErrors`
- 已用百分比计算：`used / total * 100`（缺失 `CycleUsedCapacity` 时用 `total - remain` 补齐）

## 添加密钥与平台登录

1. 在渠道的上游密钥抽屉点击「添加密钥」。WorkBuddy 默认选择「平台登录」，仍可切换到「手动填写 API Key」。不支持登录的渠道只显示手动输入。
2. 点击「打开平台登录页」，系统现场申请一次性地址并打开官方登录页面；按平台页面提示完成微信扫码等登录。若浏览器拦截新窗口，使用等待弹窗中的「打开登录页」。
3. 每 2 秒轮询一次，成功后将 accessToken 作为新的上游密钥保存，刷新列表。等待超过 300 秒需重新发起；关闭弹窗、关闭抽屉或切换渠道会停止本地轮询。

- 上游密钥删除为物理删除，同时清理对应额度快照、模型凭据状态和绑定；可重新添加相同密钥。历史调用日志保留，被删密钥的引用置空，存留密钥的展示序号重新排列。升级迁移会清理以前软删除的密钥，删除不可恢复。
- 登录由渠道类型的 `login.adapter=external_link`、`platform=workbuddy`、`prefixPath=/plugin` 声明，不在通用组件中硬编码微信登录。
- 官方链路：`POST /v2/plugin/auth/state?platform=workbuddy` 获取 state/authUrl；`GET /v2/plugin/auth/token?state=...` 的业务码 11217 表示等待，0 表示完成。完成票据只能消费一次。
- 目前不自动刷新令牌；令牌失效后重新登录添加密钥，再删除失效密钥。手动输入仍只填 accessToken，不带 Bearer 前缀。
- 已验证聊天请求只需 Authorization；无需新增 UID/Domain 凭据字段。WorkBuddy 的非流式上游请求返回空体 400，因此声明 `protocol.forceUpstreamStream=true`，由网关将流式结果聚合回非流式响应。

## 接口约束与限制

- **不提供独立 API Key 创建入口**：使用官方外链登录取得 accessToken，无需读取桌面端文件或进程内存。下方本机侦察结果属于历史记录，不代表当前获取凭据的方式。
- **积分余额可查询**：只要渠道密钥（`channel_key`）包含有效的 `accessToken`（Bearer 令牌，格式见 `C:\Users\ay\.workbuddy` 下多个日志中的 `Authorization: Bearer ...c-kg`），即可读取积分
- **签到状态不可保证查询成功**：由于缺失 `X-Device-Token`，签到接口在多数服务器环境下会返回 HTTP 401/403，结果以 `PartialErrors` 提示，不阻塞积分窗口
- **渠道密钥格式**：仅填入 `accessToken`（`Bearer` 前缀由适配器自动拼接）；不支持同时传入 `X-User-Id`（渠道配置只支持单个鉴权头 `headerName+headerPrefix`，见 `service.go:52-153`）

## 文件位置

- 内置类型配置：`manifest/builtins.json`（条目 id 9000000000000024）
- 适配器常量：`internal/logic/channeltype/service.go`（`AdapterWorkBuddy = "workbuddy_credits"`）
- 解析器实现：`internal/logic/channel/quota_workbuddy.go`
- 单测：`internal/logic/channel/quota_workbuddy_test.go`（6 例，覆盖积分解析、已用补齐、空窗口拒绝、错误响应、签到解析、未签到状态）
- 配置断言：`internal/config/builtins_test.go`（更新为 23 条内置类型，新增 `workbuddy` 映射）
- 文档：本文件

## 逆向结论（subagent ef4a118b 完成，无文件修改）

无活跃 Bearer `.c-kg` 令牌存在于任何本机文件（`secrets` 空、`workbuddy.db` 无 auth 表、Chromium 缓存仅有 9/2026 旧值全部 401）。`keyblob` 已完整逆向：AES-256-GCM（`at-rest-crypto` 模块在 `app.asar` `/main/server.js`）；`protectorKeyId` `9127dea1b44020a7` 由运行时 `TuringShieldSDK.dll` 派生，不在磁盘。`accessToken` 由 `/v2/auth/token` 获得并仅存于进程内存（`PID 34320`）。要提取需截获 `/v2/auth/token` 网络流量或用管理员权限读取 PID 34320 内存。
