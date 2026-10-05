# WorkBuddy（腾讯桌面 AI Agent / CodeBuddy 系）渠道适配

新增内置渠道类型（`workbuddy`，id `9000000000000024`）。适配桌面端 `WorkBuddy.exe`（进程 PID 34320）的官方后端接口，无第三方 SDK 授权。

## 上游来源（本机侦察）

- 桌面端：`D:\Program Files\WorkBuddy\WorkBuddy.exe`
- 进程运行中，无 `--remote-debugging-port`，无法 attach 提取运行时 token
- 数据目录：`C:\Users\ay\.workbuddy`（90 项：`workbuddy.db`、`models.json`、`keyblob`（336B DPAPI wrapped-key）、`secrets` 为空、`user-state.json`、`qimei-cache.json`、账号目录 `c409dd35-abd0-469a-aaca-a69f7aa67d6d`）
- `keyblob` 结构：`{"version":1,"keyId":"bb9f264ee53ec068","slots":[{"type":"static-v1","protectorKeyId":"9127dea1b44020a7","wrapped":"..."}]}` —— 真实凭证由本机 DPAPI 保护的静态密钥加密，无法在服务端复现
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

## 接口约束与限制

- **无第三方授权接口**：桌面端没有 `API 密钥`、`创建密钥` 或 `device_code` / `tokenUrl` 登录授权接口；`keyblob` 的 `wrapped` 字段由本机 DPAPI 静态密钥加密，无法在服务端解密
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
