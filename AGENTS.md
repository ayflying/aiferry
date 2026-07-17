# AiFerry 工作规则

## 开发与安全

- 默认使用中文沟通；Git 提交标题和正文必须使用中文，并具体说明改动、原因与验证结果。
- 后端使用 GoFrame v2：接口在 `api`，控制器只处理输入输出，业务逻辑在 `internal/service`；DAO、DO、entity 只能通过远程 `gf gen dao` 生成，禁止手工修改。
- 前端使用 Vue 3、Element Plus、Pinia 和 Vue Router，管理端保持高密度、全宽布局；模型价格按公开模型维护，渠道只作为同步来源。
- `.env` 包含数据库、Redis、Casdoor、主密钥和渠道密钥，严禁读取输出、提交或复制到源码归档。`.env.example` 只保留脱敏示例。
- 管理端由 Casdoor 保护，只允许管理员和 `AI用户组` 用户登录；登录页不展示准入用户组说明。

## 远程构建与发布

- 禁止在本机安装 Go、Node、MySQL 或 Redis。所有检查、镜像构建和部署均在 `root@192.168.50.217` 完成。
- 源码必须以独立临时目录同步到远程；构建结束后清理该临时源码和临时归档，不执行全局 Docker prune。
- 宿主机服务端口固定为 `38517`，容器内仍监听 `8080`。部署后验证 `http://127.0.0.1:38517/healthz`。
- 生产 Compose 只能引用 `ghcr.io/ayflying/aiferry:<版本>`，不得包含 `build`。运行前执行 `docker compose pull aiferry`，再执行 `docker compose up -d --no-deps aiferry`。

## 版本与镜像

- 根目录 `VERSION` 是唯一发布版本来源，当前初始版本为 `0.1.2`，格式固定为 `主版本.次版本.补丁版本`。
- 每次发布构建必须先递增补丁版本，提交并推送 Git 源码，再构建和推送同名版本标签及 `latest` 到 GitHub Container Registry。
- 使用 `hack/release.ps1` 发起发布：它只在本地 Git 工作区干净时运行，递增版本、创建详细中文发布提交、推送 `main`，再将 Git 归档同步到远程构建服务器。
- 远程服务器必须预先执行 `docker login ghcr.io -u ayflying`，并使用具有 `write:packages`、`read:packages` 权限的 GitHub PAT。令牌只存在 Docker 凭据存储，不得写入仓库或 `.env`。
- 发布后确认版本镜像与 `latest` 均已推送，Compose 已从 GHCR 拉取镜像，容器状态为 `healthy`。仓库没有远端时不得自行推送；已有 `origin` 后推送 `main`。
