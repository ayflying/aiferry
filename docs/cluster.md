# 分布式集群

一份数据库、一次配置多处生效。主节点持有 MySQL、Redis 和管理端；子节点只跑 `/v1` 转发，
用内置 SQLite 保存配置镜像与本地用量，靠集群密钥向主节点认证。

## 角色

| 角色 | `AIFERRY_ROLE` | 依赖 | 暴露的接口 |
| --- | --- | --- | --- |
| 主节点 | 不设或 `master` | MySQL、Redis、Casdoor | `/v1`、`/api/admin`、`/api/auth`、管理端页面、`/internal/cluster/*` |
| 子节点 | `worker` | 无外部依赖，仅本地数据卷 | `/v1`、`/internal/cluster/notify` |

`AIFERRY_ROLE` 不设即为主节点，且未配置 `AIFERRY_CLUSTER_SECRET` 时不注册任何内部接口，
与单机部署逐字节一致；回滚只需把角色的环境变量去掉。

## 配置项

| 变量 | 适用 | 说明 |
| --- | --- | --- |
| `AIFERRY_CLUSTER_SECRET` | 双方 | 至少 32 字节的随机串，用于 HMAC 签名，必须一致 |
| `AIFERRY_MASTER_URL` | 子节点 | 主节点地址，例如 `http://aiferry:8080` |
| `AIFERRY_NODE_NAME` | 子节点 | 节点唯一标识，留空则用主机名 |
| `AIFERRY_DATA_DIR` | 子节点 | 内置 SQLite 目录，默认 `/app/data` |
| `AIFERRY_SYNC_INTERVAL` | 子节点 | 配置同步周期，默认 60 秒 |
| `AIFERRY_FLUSH_INTERVAL` | 子节点 | 用量回传周期，默认 30 秒 |
| `AIFERRY_FLUSH_BATCH_SIZE` | 子节点 | 单批回传行数，默认 500 |
| `AIFERRY_OUTBOX_MAX_ROWS` | 子节点 | 待回传堆积告警阈值，默认 50000 |
| `AIFERRY_BOOTSTRAP_REQUIRED` | 子节点 | 启动时主节点不可达是否拒绝启动，默认 true |

## 协议

所有节点间请求都带 `X-Aiferry-Node/-Ts/-Nonce/-Signature` 头，签名内容为
`node\nts\nnonce\nMETHOD\npath\nsha256(body)`，时间窗 ±5 分钟，nonce 在接收侧缓存 15 分钟防重放。
时间戳或链接不一致都会被拒绝，因此**两台机器的时钟必须同步**。

| 接口 | 方向 | 用途 |
| --- | --- | --- |
| `POST /internal/cluster/register` | 子 → 主 | 注册节点，返回节点 ID 与当前配置版本 |
| `POST /internal/cluster/config?version=N` | 子 → 主 | 拉取全量快照，版本未变返回 304 |
| `POST /internal/cluster/heartbeat` | 子 → 主 | 上报配置版本、待回传行数、最近一次错误 |
| `POST /internal/cluster/usage` | 子 → 主 | 批量回传用量明细 |
| `POST /internal/cluster/notify` | 主 → 子 | 配置版本变化通知，子节点收到后立即同步 |

## 配置同步

- 管理端在渠道、凭据、模型、价格、系统参数、访问密钥、用户余额等写路径成功后调用
  `cluster.NotifyConfigChanged`，递增 `cluster_state.config_version` 并推送 `notify`。
- 主节点另有一个 120 秒的指纹巡检（16 张配置表的行数与最大 `updated_at`），
  兜底捕捉漏掉的递增，代价是最多 2 分钟延迟。
- 子节点收到通知或到达同步周期后拉全量快照（gzip），在一个事务里完成 upsert + 剪枝。
- 子节点本地只保留自己的健康观测列（`channel_models.health_score` 等）与
  `channel_model_credentials` 的冷却状态；其余列全部以主节点为准。

## 账务

- 余额与密钥累计消耗以主节点为权威值，快照会覆盖子节点的本地记账，
  因此**最多允许一个同步周期的超支**。
- 子节点本地写 `usage_logs`，每 `AIFERRY_FLUSH_INTERVAL` 批量回传；主节点确认后才删除本地行，
  失败自动重试，不会因主节点短暂不可用丢账。
- 主节点用 `cluster_usage_batches(node_id, batch_id)` 唯一键与 `usage_logs.request_id` 唯一键
  做双重幂等：同一批次重放、跨批次重复行都不会二次扣费。
- 只有 `billing_details_json.charged=true` 的行才扣费，自有渠道等只统计不扣费。

## 已知限制

- 子节点的健康检查结果不共享：各节点独立健康评分与自动禁用，`channels.status`
  以主节点为准，子节点的本地自动禁用会在下一次快照被主节点状态覆盖。
- `/v1/dashboard/billing/*` 与 `/v1/video/*` 尚未跨节点安全：前者读取本节点账单视图，
  后者依赖本节点持有的上游任务映射。
- 子节点不提供管理端页面与 `/api/*`，所有配置只能在主节点修改。

## 部署要点

- 主节点必须先于子节点启动；子节点在 `AIFERRY_BOOTSTRAP_REQUIRED=true` 时注册失败会直接退出。
- 子节点不需要 MySQL / Redis / Casdoor 的任何凭据，误配会直接报错而不是静默忽略。
- 子节点的 `AIFERRY_DATA_DIR` 必须是持久卷，否则重启后需要重新全量同步。
- 主节点「节点管理」接口：`GET /api/admin/cluster/nodes` 列出节点与是否落后，
  `DELETE /api/admin/cluster/nodes/:id` 吊销节点，被吊销的节点注册会被拒绝。
