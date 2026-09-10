# 账单查询 API（OpenAI 兼容）

> 适用版本：0.5.86 起
> 关联代码：`internal/logic/relay/billing.go`、`internal/controller/relay/billing.go`

## 1. 概述

AiFerry 提供一组 OpenAI legacy dashboard API 兼容的账单查询端点，鉴权方式与转发接口一致（`Authorization: Bearer <访问密钥>`）。已有生态工具（余额监控脚本、ChatGPT-Next-Web、one-api 系面板等）无需改造即可接入。

三个端点：

| 端点 | 鉴权 | 用途 |
|---|---|---|
| `GET /v1/dashboard/billing/subscription` | 任意访问密钥 | 查询密钥可用额度（用户余额与密钥限额取小值） |
| `GET /v1/dashboard/billing/usage` | 任意访问密钥 | 查询该密钥在时间窗口内的用量（日聚合） |
| `GET /v1/dashboard/billing/channels` | 仅管理员角色的密钥 | 查询全部渠道的最新余额快照 |

## 2. 密钥余额 — `GET /v1/dashboard/billing/subscription`

```bash
curl -H "Authorization: Bearer sk-xxx" https://your-gateway/v1/dashboard/billing/subscription
```

响应：

```json
{
  "hard_limit_usd": 92.5,
  "system_hard_limit_granted": 0,
  "access_until": 1797000000,
  "has_payment_method": true,
  "plan": { "title": "Pay as you go", "is_paid": true }
}
```

语义说明：

- `hard_limit_usd` = min(用户总余额, 密钥剩余限额)。密钥未设 spend limit 时即用户总余额；设置了 limit 时与扣费侧约束一致（密钥超限请求会被拒绝）。
- 金额为本网关的计费口径（估算费用），单位随系统定价配置，字段名保留 `_usd` 以兼容 OpenAI 客户端。

## 3. 密钥用量 — `GET /v1/dashboard/billing/usage`

参数：`start_date`、`end_date`（格式 `YYYY-MM-DD`，含头不含尾；end 缺省为今天，start 缺省为 30 天前，窗口最长 100 天）。

```bash
curl -H "Authorization: Bearer sk-xxx" \
  "https://your-gateway/v1/dashboard/billing/usage?start_date=2026-09-01&end_date=2026-09-10"
```

响应：

```json
{
  "object": "list",
  "total_usage": 1.234,
  "daily_costs": [
    { "timestamp": 1788240000, "n_requests": 42, "num_model_invocations": 42, "n_tokens_1k": 15.2 }
  ],
  "total_lines": 1,
  "total_tokens": 15200,
  "total_requests": 42
}
```

- 数据来源 `usage_logs`（与控制台用量页同一事实来源），按日聚合该密钥的请求。
- `total_usage` 为窗口内估算费用合计。

## 4. 渠道余额（管理员）— `GET /v1/dashboard/billing/channels`

仅当密钥所属用户具备管理员角色时可用；普通密钥返回 403。

```bash
curl -H "Authorization: Bearer sk-admin-xxx" https://your-gateway/v1/dashboard/billing/channels
```

响应：

```json
[
  {
    "channelId": 1,
    "name": "智谱 GLM",
    "type": "zhipu",
    "status": 1,
    "used": 12.4,
    "remaining": 87.6,
    "currency": "CNY",
    "queriedAt": "2026-09-10T08:00:00+08:00"
  }
]
```

- 数据来自成本同步任务写入 `channels` 表的 `LastCost*` 快照字段（默认周期同步），**不触发上游实时查询**，适合高频轮询监控。
- 需要实时额度（如智谱积分窗口）请走管理端控制台的「查询额度」按钮（实时上游查询）。

## 5. 错误码

| HTTP | 场景 |
|---|---|
| 401 | 缺少或无效的 Bearer 密钥 |
| 400 | 请求参数错误（如日期窗口倒置、超过 100 天） |
| 403 | 密钥所属用户不是管理员（仅 `/billing/channels`） |
| 500 | 服务器内部错误 |
