# 生产发布与故障复盘（2026-10-29）

本文记录一次真实的整站中断事故、根因与之后的发布规程。发布动作本身有破坏性，读一次能少踩一次。

## 事故经过

| 时间（本地） | 事件 |
| --- | --- |
| 00:49 | 用 Portainer 栈更新把生产从 0.6.49 升到 0.6.52 |
| 00:49 | `PUT /api/stacks/22` 返回成功，随后 `/healthz` 变成 nginx 502 |
| 00:49–02:27 | 站点整站不可用，期间反复受 Portainer 响应超时干扰 |
| 02:27 | 恢复 0.6.49，`/healthz` 正常 |
| 10:22 | 用安全脚本重新发布 0.6.52 |
| 10:27 | 部署完成，`version=0.6.52`，容器 healthy |

整站中断约 1 小时 15 分。

## 根因（两个叠加）

1. **直接原因：栈环境变量被写成空名。**
   `GET /api/stacks/22` 返回的 `Env` 只有一个空白条目 `{name:"", value:""}`。发布时把它原样带回，
   Portainer 依此重写 `stack.env`，内容变成一行 `=`。compose 解析 `env_file` 直接报
   `invalid environment variable: =`，容器无法创建。

2. **放大原因：Portainer 栈更新是「先删旧容器、再建新容器」。**
   旧容器删除后新容器创建失败，就没有任何进程监听 38517，nginx 一律返回 502。
   这不是灰度发布，没有失败时继续服务的能力。

另外宿主机 Docker 网络层还有残留：`failed to create network … failed to add bridge … to ipset: exist`，
说明删除过的网桥仍留在 ipset 里。给栈固定一个未占用的网段（`172.25.0.0/16`）才能建网络。

## 恢复为什么慢

- 没有备份 `stack.env`，恢复时必须从本机 `.env` 回填 11 个变量。
- 一开始按「镜像坏了 / 容器崩了」排查，方向错；真正原因写在 `DeploymentStatus.Message` 里，
  应当第一步就读它。
- Portainer 间歇性 40～120 秒无响应，每步都要多轮重试。

## 现在的发布规程

发布统一用 `hack/deploy-stack.ps1`，不再手写 `PUT`：

```powershell
# 先演练：只做校验、备份，不发请求
.\hack\deploy-stack.ps1 -Version 0.6.53 -BaselineVersion 0.6.52 -DryRun

# 再正式发布
.\hack\deploy-stack.ps1 -Version 0.6.53 -BaselineVersion 0.6.52
```

脚本强制以下顺序：

1. 读线上 `/healthz`，版本与 `-BaselineVersion` 不一致立即中止（防止覆盖别人的发布）。
2. 读栈 `Env`，任何空名/非法名立即拒绝（堵住本次事故的直接原因）。
3. 读栈 compose，校验 `services` 段、唯一 `image` 行、固定版本标签。
4. 把 compose 与 env 原样备份成 `payload-<时间>.json`，回滚直接重放该文件。
5. `PUT` 更新后按栈状态判定：状态 4（失败）立即回滚，仍在部署（3）继续等待，默认窗口 600 秒。
6. 回滚后仍按 `/healthz` 的 `version` 校验，回到基线才算结束；否则报「需要人工介入」。

## 硬性约束（写进流程）

- **不要给栈的 `Env` 传空数组或空白条目**：空数组会覆盖 `stack.env`，空白条目会写出 `=`。
- `stack.env` 现在 11 个变量：`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_DATABASE`、`MYSQL_USER`、
  `MYSQL_PASSWORD`、`REDIS_ADDR`、`REDIS_PASSWORD`、`AIFERRY_MASTER_KEY`、`CASDOOR_ENDPOINT`、
  `CASDOOR_CLIENT_ID`、`CASDOOR_CLIENT_SECRET`。值只在本地 `.env` 与本机备份中出现，不写入文档、不打印。
- 上线判定只认 `https://aiferry.yunloli.cn/healthz` 的 `version` 与 `revision`；
  「镜像构建成功」和「栈 PUT 返回 200」都不算发布成功。
- 拉取固定版本镜像可能耗时数分钟，等待窗口不要设太短，否则会把正常部署误判成失败。

## 后续可改进

当前规程能把失败压缩到秒级，但仍有一次短暂中断窗口（删旧容器到新容器就绪）。
要做到真正零中断，常见做法是让 nginx 指向一个可切换的上游端口，先在新端口起新容器、
健康后再切流量；这需要改宿主机反代配置，属于独立的一次改动，未在本次实施。