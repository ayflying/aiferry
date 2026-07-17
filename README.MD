# AiFerry

AiFerry（AI 摆渡）是基于 GoFrame v2 和 Vue 3 的 OpenAI 兼容中转管理工具。第一阶段提供渠道管理、模型发现与映射、模型测试、上游费用查询、访问密钥和用量统计。

## 支持范围

- OpenAI 兼容接口：`/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`
- 普通响应与 SSE 流式响应
- 渠道优先级、权重路由、最多三渠道故障转移和 Redis 冷却
- OpenAI 官方、Sub2API 和自定义 JSON 费用查询
- Casdoor OAuth2 登录和 Redis Session

`/v1/*` 使用 AiFerry 签发的 `af_...` Bearer 密钥。`/api/admin/*` 使用 Casdoor 登录 Session，两套认证相互独立。

## Casdoor 权限

只有以下 Casdoor 用户可以进入管理端：

- Casdoor 管理员；
- 用户组名称去掉 `组织/` 前缀后，精确等于 `AI用户组` 的用户。

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
CASDOOR_ALLOWED_GROUP / SESSION_TTL_HOURS
AIFERRY_IMAGE / AIFERRY_VERSION / AIFERRY_PORT
```

`AIFERRY_MASTER_KEY` 必须是 Base64 编码的 32 字节随机密钥。渠道推理密钥和管理密钥由它使用 AES-GCM 加密。

## 远程开发

项目不要求在本机安装 Go、Node、MySQL 或 Redis。源码同步到构建服务器后，统一通过开发 toolbox 执行：

```bash
docker compose -f docker-compose.dev.yml run --rm toolbox sh ./hack/remote-tools.sh migrate
docker compose -f docker-compose.dev.yml run --rm toolbox sh ./hack/remote-tools.sh dao
docker compose -f docker-compose.dev.yml run --rm toolbox sh ./hack/remote-tools.sh backend-test
docker compose -f docker-compose.dev.yml run --rm toolbox sh ./hack/remote-tools.sh frontend-check
```

DAO、DO 和 entity 必须由远程 `gf gen dao` 生成，禁止手工修改生成文件。

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

根目录 `VERSION` 是唯一版本来源，初始版本为 `0.1.2`。每次发布构建均递增补丁版本，先提交并推送 Git 源码，再由远程构建服务器构建、推送和部署镜像。

首次发布前，在 `root@192.168.50.217` 上登录 GitHub Container Registry：

```bash
docker login ghcr.io -u ayflying
```

使用具有 `write:packages` 和 `read:packages` 权限的 GitHub PAT，令牌不得写入仓库或 `.env`。随后从 Windows PowerShell 在仓库根目录执行：

```powershell
.\hack\release.ps1
```

脚本要求 Git 工作区干净，并自动完成版本递增、详细中文发布提交、`main` 推送、临时源码同步、远程 `docker build`、`ghcr.io/ayflying/aiferry:<版本>` 与 `latest` 推送、Compose 拉取和健康检查。远程临时源码会在结束时清理。

## 测试

- Go 单元和集成检查：`hack/remote-tools.sh backend-test`
- Vue 类型检查、Vitest 和生产构建：`hack/remote-tools.sh frontend-check`
- Casdoor 脱敏冒烟：`sh hack/smoke-auth.sh`
- Playwright 桌面与移动验收：在 Playwright 容器中执行 `npm run test:e2e`

项目参考 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 和 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的协议与产品边界，不复制其 AGPL-3.0 实现代码。

## 开源协议

AiFerry 使用 [MIT License](LICENSE) 开源。
