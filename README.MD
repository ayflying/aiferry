# AiFerry

AiFerry（AI 摆渡）是基于 GoFrame v2 和 Vue 3 的 OpenAI 兼容中转管理工具。第一阶段提供渠道管理、模型发现与映射、模型测试、上游费用查询、访问密钥和用量统计。

## 支持范围

- OpenAI 兼容接口：`/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`、`/v1/images/generations`
- 普通响应与 SSE 流式响应
- 模型列表能力元数据：上游同步、公开模型手动覆盖、输入输出模态与三态能力、上下文长度（见 [模型能力元数据](docs/model-metadata.md)）
- Responses 与 Chat Completions 之间的协议自动转换（工具调用历史、多模态内容与思考内容的双向还原，见 `docs/protocol-conversion.md`）
- 渠道优先级、权重路由、最多三渠道故障转移和 Redis 冷却
- 渠道模型定时关闭：按「渠道 × 模型」配置星期与时段，关闭期间自动从路由摘除（见 `docs/model-closed-windows.md`）
- 渠道并发限制：按「渠道 × 密钥」隔离的转发并发上限，满额先换空闲密钥再排队等待（见 `docs/channel-concurrency-limit.md`）
- 渠道组织 / 项目标识：仅 OpenAI 官方渠道展示，转发时注入 `OpenAI-Organization` / `OpenAI-Project` 头，成本查询按 `project_ids` 过滤（见 `docs/channel-organization-identity.md`）
- thinking 模式模型的 `reasoning_content` 回传（DeepSeek/Kimi 等要求发生过工具调用后必须回传，见 `docs/reasoning-echo.md`）
- 展示货币统一换算：跨币种渠道的费用与余额合并成一个币种显示，汇率可自动抓取或人工指定（见 `docs/currency-display.md`）
- OpenAI 官方、Sub2API 和自定义 JSON 费用查询
- Casdoor OAuth2 登录和 Redis Session

`/v1/*` 使用 AiFerry 签发的 `sk-...` Bearer 密钥。`/api/admin/*` 使用 Casdoor 登录 Session，两套认证相互独立。历史 `af_...` 密钥仍可继续调用，建议按需轮换为新格式。

## Casdoor 权限

所有启用的 Casdoor 用户都可以进入管理端。Casdoor 管理员会自动获得 AiFerry 管理员权限，其他用户为普通用户。

AiFerry 使用 Casdoor `uid`（缺失时使用 `id`）作为唯一外部身份，不使用邮箱关联账户。被禁用、禁止或删除的用户会被拒绝。OAuth `state` 保存在 Redis 并与 HttpOnly Cookie 双重校验。

回调地址根据请求的 `Host` 和 `X-Forwarded-Proto` 自动生成，固定路径为：

```text
https://your-aiferry.example.com/auth/casdoor/callback
```

需要在 Casdoor 应用的 Redirect URLs 中配置对应地址。反向代理部署时必须正确传递 `Host` 和 `X-Forwarded-Proto`。

## 配置

复制 `.env.example` 为 `.env` 后填写运行环境配置。`.env` 已被 Git 和 Docker 构建上下文忽略，不得提交。

关键配置：

```text
MYSQL_HOST / MYSQL_PORT / MYSQL_DATABASE / MYSQL_USER / MYSQL_PASSWORD
REDIS_ADDR / REDIS_PASSWORD
AIFERRY_MASTER_KEY
CASDOOR_ENDPOINT / CASDOOR_CLIENT_ID / CASDOOR_CLIENT_SECRET
AIFERRY_ADMIN_ROLES
```

`AIFERRY_MASTER_KEY` 必须是 Base64 编码的 32 字节随机密钥。渠道推理密钥和管理密钥由它使用 AES-GCM 加密。

登录会话有效期固定 30 天，每次请求都会滑动续期（刷新 Redis TTL 和 Cookie），不提供环境变量配置。历史上可用 `SESSION_TTL_HOURS` 覆盖，曾被压到十几小时导致隔天掉登录，现已移除。

## 本机基础检查

使用本机已有 Go、Node.js 和前端依赖运行基础检查；不再要求把源码同步到远程构建机：

```powershell
go test ./...
pnpm --dir frontend run typecheck
pnpm --dir frontend run test:run
pnpm --dir frontend run build
```

DAO、DO、entity 与服务接口必须使用 GoFrame 生成工具生成，禁止手工修改。需要数据库的迁移或集成验证应使用隔离测试数据库，不使用生产凭据。本机单元测试不等于生产验收。

## 运行

生产运行使用 GitHub Container Registry 中的已发布镜像，不在运行服务器重新构建：

```bash
docker compose pull aiferry
docker compose up -d --no-deps aiferry
```

如需在本地 Compose 网络中启动 MySQL 8.4 和 Redis 7.4，先确保 `.env` 已设置镜像名和版本：

```bash
docker compose --profile local up -d
```

健康检查：`GET /healthz`。管理端默认监听 `http://localhost:38517`。

## 发布镜像

根目录 `VERSION` 是唯一版本来源，初始版本为 `0.1.2`。**版本号在每次提交发布前手动递增（补丁位 +1），与代码改动一并提交**；CI 只校验格式并用它打同名版本标签和 `latest` 标签，不再自动递增或回推 bump 提交。提交并推送 Git 源码后，由 CI 构建并推送镜像，再由部署流程拉取更新。

基础测试通过后，仅暂存本次改动（含手动递增的 `VERSION`），创建中文提交并推送 `main`。正式版本由 GitHub Actions CI 自动构建，发布版本标签和 `latest`；镜像凭据由 CI Secrets 管理，不得写入仓库或 `.env`。

`hack/release.ps1` 是历史远程发布脚本，当前常规流程不使用。CI 成功不代表生产容器已经更新；生产部署与健康检查是独立验收步骤，需明确授权。

## 测试

- Go 单元检查：`go test ./...`
- Vue 类型检查、Vitest 和生产构建：`pnpm --dir frontend run typecheck`、`pnpm --dir frontend run test:run`、`pnpm --dir frontend run build`
- Casdoor 脱敏冒烟：`sh hack/smoke-auth.sh`
- Playwright 桌面与移动验收：在 Playwright 容器中执行 `npm run test:e2e`

项目参考 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 和 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的协议与产品边界，不复制其 AGPL-3.0 实现代码。

## 开源协议

AiFerry 使用 [MIT License](LICENSE) 开源。
