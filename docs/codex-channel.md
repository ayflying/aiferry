# Codex 渠道

## 适用范围与限制

内置类型 `codex` 连接 ChatGPT 订阅下的 Codex 后端，不是 `api.openai.com` 的 API Key 渠道。该后端是官方客户端使用的内部协议，**并非稳定公共 API**；上游路径、请求约束、套餐与授权政策可能随版本变化。本实现固定模型发现客户端版本 `0.101.0`，不代表声明兼容未来所有版本。

登录使用官方设备授权适配器 `codex_device`。设备登录属于 beta 能力，需要用户在 ChatGPT 安全设置中启用；组织账号可能还需要管理员开启相应权限。没有开启、套餐不支持或组织策略禁止时，登录/调用均可能失败。不要把 ChatGPT 网页 Cookie 当作 API Key，不应复制浏览器会话、绕过组织授权，也不要在日志或配置示例里公开 OAuth token。

## 内置配置

| 项目 | 配置 |
| --- | --- |
| 类型 code | `codex` |
| API 根地址 | `https://chatgpt.com/backend-api/codex` |
| 登录 | `login.adapter=codex_device` |
| 模型发现 | `GET /models?client_version=0.101.0`，`listPath=models`、`idPath=slug` |
| 推理端点 | 仅声明 `responses`：`POST /responses`，JSON，支持流式 |
| 协议 | `forceUpstreamStream=true`；不声明 Chat Completions、Messages、音频或视频端点 |
| 套餐额度 | `quota.adapter=codex_usage`，`GET /backend-api/wham/usage` |
| 金额/价格同步 | `none`，不从套餐百分比推算金额或 token 数 |

模型与推理路径拼接在 API 根地址之后。额度路径按同一 host 根路径解析，所以最终额度 URL 为 `https://chatgpt.com/backend-api/wham/usage`，**不是** `/backend-api/codex/backend-api/wham/usage`。

模型发现和额度查询复用现有上游 JSON 请求链路；OAuth 凭据的解包、账户头和令牌刷新由通用上游鉴权链路负责，额度适配器不另存、不直接解析 token。

## 登录与使用

1. 创建渠道，选择「官方 Codex 套餐」，不填写 API Key。
2. 打开渠道凭据管理，选择平台登录；在官方页面输入本页显示的一次性设备码，并完成 ChatGPT 账号授权。
3. 授权有效期为 15 分钟，本页按官方轮询间隔等待；成功后自动保存账号凭据，同一渠道再次授权同账号会更新原凭据。
4. 同步账号实际可用模型，通过「套餐额度」查看窗口比例和重置时间。模型调用使用 Responses；Chat Completions 请求需开启协议转换。音频、视频、图片和嵌入等非文本独立端点不支持。

访问令牌和刷新令牌使用现有主密钥加密存储，设备会话也加密并绑定当前用户及渠道。访问令牌到期前自动刷新，采用共享缓存锁与数据库条件更新防止并发消费轮转令牌或覆盖重新登录结果；授权撤销后需要重新登录。登录凭据只允许发送到官方 ChatGPT 对应接口，不跟随重定向。请求体按已核对的官方客户端版本适配强制流式和不存储，非流式客户端由网关聚合返回。

本次没有替用户登录真实 ChatGPT 账号，不读取本机已有登录文件；本机测试使用脱敏夹具，真实账号权益和上游是否接受授权仍需账号授权后验收。

## 额度语义

WHAM 的 `plan_type` 原样作为套餐档位显示，不硬编码套餐枚举。主额度读取 `rate_limit.primary_window` 与 `rate_limit.secondary_window`，窗口可缺失或为 `null`：

- `used_percent`：已使用百分比。缺失时不凭空生成一个 0% 窗口；显式 `0` 是有效值。
- `limit_window_seconds`：窗口持续秒数，用于展示实际周期和多账号窗口分组；不假定主窗口一定是 5 小时、次窗口一定是 7 天。不同套餐（例如 Pro）可能没有传统 5 小时窗口。
- `reset_at`：Unix **秒**时间戳，优先使用，不按毫秒解析。
- `reset_after_seconds`：当没有有效 `reset_at` 时，相对本次查询时间计算重置时刻；没有重置字段就保持未知。

同时支持官方 `additional_rate_limits` 数组中的 `metered_feature`、`limit_name` 和嵌套 `rate_limit`，独立展示附加功能额度；兼容部分上游返回的 `code_review_rate_limit` 独立代码审查窗口，不将其混入普通 Codex 窗口。可选 `normal_model_slug` 等未用字段不影响解析。

官方 `credits.balance` 是字符串形式的积分，不是可直接等同美元的余额。本阶段不转换积分，不给百分比伪造 `used`/`total`/`remaining` 金额或 token 数，也不用于金额提醒。没有任何可识别窗口时返回明确错误，而非把未知额度展示成充足。

渠道级查询沿用现有一分钟缓存、多密钥并发及部分错误回报；按窗口类别与实际持续时间分组，对拥有该窗口的账号取百分比平均，重置时间取最早。该总览是多账号水位概览，不代表这些账号共享一笔可相加的额度；精确诊断请查询单个凭据。

## 官方源码核对

核对版本：`openai/codex` 的 `rust-v0.101.0`。

- [backend-client/src/client.rs](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/backend-client/src/client.rs)：ChatGPT backend-api 风格请求 `/wham/usage`，解析主/附加额度。
- [rate_limit_status_payload.rs](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/codex-backend-openapi-models/src/models/rate_limit_status_payload.rs)：`plan_type`、可空 `rate_limit`、`credits`、`additional_rate_limits`。
- [rate_limit_status_details.rs](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/codex-backend-openapi-models/src/models/rate_limit_status_details.rs)：可空主、次窗口。
- [rate_limit_window_snapshot.rs](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/codex-backend-openapi-models/src/models/rate_limit_window_snapshot.rs)：百分比、窗口秒数和重置字段。
- [additional_rate_limit_details.rs](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/codex-backend-openapi-models/src/models/additional_rate_limit_details.rs)：附加额度功能标识及展示名称。

该版本官方 payload 未声明 `code_review_rate_limit`；本项目仅作兼容读取，不声称它是这一版的必需字段。

## 本机验证

运行 `go test ./internal/logic/channeltype ./internal/logic/channel ./internal/config`。测试使用内置清单与脱敏 JSON 夹具，不访问真实 ChatGPT 账号、不读取环境密钥、不要求 MySQL/Redis。单测通过不等于实际账号授权、套餐权益和上游稳定性验收。
