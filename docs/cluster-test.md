# 集群隔离验收（仅测试，禁止复用生产配置）

## 边界与前提

- 本方案不 SSH、不调用任何现有生产部署脚本、不连接线上，不改根 Dockerfile / VERSION。由 lead 在 dev-server 执行。
- Compose project 固定 `aiferry-cluster-test`；自有 internal 网络、自有 MySQL 8.4 / Redis 7.4 / master / worker 数据卷。MySQL、Redis、Python mock 不发布端口；声明的宿主 HTTP 入口仅为 `127.0.0.1:39517`（master）、`127.0.0.1:39518`（worker）。部分 Docker Engine 的 internal bridge 不生成宿主端口绑定：脚本会只读 inspect 本 project 的 master/worker 容器内部 IP，以宿主到该独立 bridge 的直连 HTTP 兼容；不改网络、不向外发布端口。不得改成 `0.0.0.0`，也不使用生产 `38517`。
- master 与 worker 使用完全相同的完整后端验收镜像，不是根 Dockerfile 的 frontend-only 镜像。Casdoor 只填 `127.0.0.1:1` 测试占位，满足配置验证；不启动 Casdoor、不测试管理端登录。
- 需要 Docker Engine / Compose v2（支持 `up --wait`）、Python 3.10+；脚本仅使用 Python 标准库。不用 pip、不安装宿主 MySQL/Redis。
- 数据库由后端正常执行仓库 migrations 建立。种数 helper 仅生成独立测试 fixture：用户/APIKey/渠道/凭据/公开模型/价格，ID `990001`，模型 `cluster-test-model`，上游仅 `http://mock:8090`，初始余额 100。渠道所有者使用独立管理员 fixture 用户 `990002`；用户 `990001` 通过测试渠道分组授权消费非自有渠道，避免命中自有渠道免扣费策略。种数不修改生产鉴权或计费策略，不导入生产数据。

## CI 镜像协调（lead）

CI 在同一个 buildx builder / runner 上先构建完整后端并 `--load`，再构建专用测试 tag；不推送 `latest`，不改生产发布链路：

```sh
docker buildx build --platform linux/amd64 --load -f Dockerfile.full \
  --build-arg VERSION=cluster-test --build-arg VCS_REF="$GITHUB_SHA" \
  -t aiferry-cluster-test-backend:ci .
docker buildx build --platform linux/amd64 --push -f Dockerfile.cluster-test \
  --build-arg BACKEND_IMAGE=aiferry-cluster-test-backend:ci \
  -t ghcr.io/ayflying/aiferry:cluster-test-<sha> .
```

注意：使用 docker-container buildx 驱动时，第二步未必可见宿主 `--load` 的本地 base。lead 应使用 docker 驱动，或先推送**专用** backend 中间 tag/digest，再将远端引用传入 `BACKEND_IMAGE`。不因缓存可见性而误回退生产 `latest`。

`Dockerfile.cluster-test` 只在完整镜像上加入 Go 标准库 fixture helper（AES-256-GCM：随机 12-byte nonce 前缀 + ciphertext + tag，RawStdEncoding，无 padding），与现有 secret 实现兼容。所有测试凭据运行时生成，不进镜像。

## 操作顺序（由 lead 执行）

以下在 checkout 根执行。镜像提前由 CI 产出，lead 按专用 tag/digest pull；Compose 的 `pull_policy: never` 防止隐式换镜像。基础服务镜像由 Compose 按需拉取。不要运行 `docker compose config`（无 `--quiet`）以免输出插值凭据。

```sh
docker pull ghcr.io/ayflying/aiferry:cluster-test-<sha>
python3 manifest/cluster-test/cluster_test.py init --image ghcr.io/ayflying/aiferry:cluster-test-<sha>
python3 manifest/cluster-test/cluster_test.py validate
python3 manifest/cluster-test/cluster_test.py up
python3 manifest/cluster-test/cluster_test.py check
python3 manifest/cluster-test/cluster_test.py recovery
```

Windows 本机可用 `python` 替换 `python3`。凭据默认保存到用户主目录 `.aiferry-cluster-test`（外部运行态，不是仓库 `.env`）；可在子命令前加 `--state-dir /专用外部路径`。脚本拒绝仓库内路径，防止 `Dockerfile.full` 的 `COPY . .` 意外带入运行态测试密钥；不读仓库 `.env`，屏蔽 Compose 隐式 `.env` 与 shell TEST/COMPOSE 插值覆盖。Unix 新文件权限 0600、目录 0700；Windows 由 lead 另核查 NTFS ACL，不共享该目录。

`init` 不覆盖既有状态，避免旧 MySQL 卷与新密码不匹配。再次 `up` 可复用 fixture，保留余额与历史；`seed` 可单独重做，SQL 不重置余额或消费。SQL 直写后会重启 master 清缓存；worker 首次 bootstrap 前已完成种数。若对运行中的旧 worker 重新种数，等待配置同步后再验收。

### 旧测试镜像的独立分组修复

若旧 fixture 把消费用户设为渠道创建者，会产生有成本用量但免扣余额的正常业务行为。可先用更新后的 Go helper 输出**无密钥**策略修复 SQL，放到 `.git` 临时文件，由 lead 通过现有测试脚本的 `Cluster.sql()` 送入隔离 MySQL，再重启 master 并等待 worker 同步；不需要重建整镜像或读取主密钥。下一次 CI 会将 helper 正式打包。

```sh
AIFERRY_ROLE=master MYSQL_DATABASE=aiferry_cluster_test \
  go run ./manifest/cluster-test/fixture.go --repair-policy > .git/cluster-test-repair.sql
```

此模式仅校验 role 和测试数据库名，不读取 stdin、MasterKey 或运行态凭据。SQL 幂等建立 owner `990002`、测试分组及用户/APIKey 关联，更新渠道创建者与配置版本；不清除历史、不重置余额、不改 APIKey 或上游密钥。脚本用 before 增量检查，不要求删除既有失败用量。

## 自动断言覆盖

- 主从 HTTP 各发普通 chat / SSE，共 4 次，要求内容 `OK`、SSE `[DONE]` 与 usage 12 tokens。
- 通过 `docker compose exec -T mysql` 查询测试数据库，不映射 MySQL 端口；断言 request_id 唯一、主从 node_id、stream 标记、input=10 / cached=3 / output=2 / total=12。
- 使用公开模型 token 价格 input=1、cached=0.5、output=2 / 百万 tokens，每次应收费 `0.00001250`；比较用量金额、用户余额扣减、APIKey spent 增量，额外等待确保无重复扣费。
- worker 注册名 `test-worker`，回调地址必须 `http://worker:8080`；master 签名信任名显式 `master`。
- `recovery` 先执行普通验收，再停 master；worker 持续接普通/SSE，挂载**本测试 project** worker 数据卷只读，用 Python sqlite3 证明 outbox 两行。停 worker 后再次证明数据持久化，再恢复 master / worker，断言两条回传到账、余额准确、outbox 排空与无重复扣款。
- worker 的强制 bootstrap 要求 master 可达，因此不宣称“master 仍宕机时 worker 冷启动成功”；恢复顺序明确为先 master 后 worker。

脚本失败返回非零，不打印 APIKey/密钥/SQL。排查只看该 project 的容器日志，分享前脱敏；不得为验收浏览生产 `.env` 或广泛搜索秘密。此脚本只应在独占测试期间运行，其他并发 fixture HTTP 请求会使精确数量断言失败。

## worker 端点边界

worker 的请求鉴权、模型列表和转发使用本地快照；余额视图为周期同步值，不保证跨节点即时余额。当前异步视频任务、历史账单和渠道余额接口仅允许直接访问 master：worker 对 `/v1/video/*`、`/v1/videos*`、`/v1/dashboard/billing/usage`、`/v1/dashboard/billing/channels` 返回 501 `master_node_required`，不会隐式逐请求代理到 master，也不会把待回传 outbox 冒充完整账单。

集群共享 HMAC 密钥只适用于互信节点：拥有密钥的节点仍可能声明其他节点名，不提供节点之间的密码学角色隔离；生产跨网络必须使用可信网络与 TLS。`OutboxMaxRows` 当前是告警阈值，不是硬性准入上限；延迟余额允许超支，尚未证明超支严格限制为一个同步周期。渠道诊断数字的 nullable 语义尚未全部扩展，不能把测试通过解释为所有端点、所有上游均已验收。

## 停止、清理与复验

```sh
# 停止并保留所有测试数据，后续 up 可复验
python3 manifest/cluster-test/cluster_test.py down
# 明确删除本测试 project 容器、网络、数据卷（不删外部凭据）
python3 manifest/cluster-test/cluster_test.py destroy
```

`destroy` 不使用全局 prune，不触碰其他容器；新的完全空白验收应在 destroy 后由 lead 手工归档/删除已核准的**外部测试凭据目录**，再 init。不要对未知目录执行递归删除。

## lead 必须确认的事项

1. CI 专用完整镜像已包含本分支的集群修复与 fixture helper；主从使用相同 tag/digest。后端 NodeURL、MasterNodeName 参数、worker 凭据自检列名需与当前代码一致。
2. 服务器只执行本文独立命令，先确认 39517 / 39518 仍空闲；不要操作 lanet-node / act-runner / portainer_agent 或生产脚本。
3. 预拉镜像时可能需要仓库登录，凭据仍由 lead 的既有安全流程管理，不写入本文/源码/脚本。
4. 本次仅完成文件与离线检查；没有连接服务器或宣称容器验收通过。实际运行结果由 lead 收集。
