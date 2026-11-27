# AiFerry 私用 Antigravity 文本渠道

官方 `agy` 只由桥接调用，且必须是未修改的官方二进制。官方 CLI 从 1.1.15 起在未登录时不再打印授权地址，只报 `You are not logged into Antigravity`，所以「在容器里交互式跑一遍 CLI 完成登录」这条路已经不通；桥接改为按官方客户端的方式自己跑标准 OAuth 2.0 + PKCE，见下文「登录」。实现参考 [dsh-agy-link](https://github.com/amlyczz/dsh-agy-link) 与 [Google 无头模式文档](https://antigravity.google/docs/cli/headless/)。

## 编排与登录

桥接作为 `aiferry` 编排内的 `agy-bridge` 服务运行（[compose.yml](../deploy/agy-bridge/compose.yml)）。镜像是独立的通用仓库 `ayflying/aiferry-bridge`，**不含任何官方 CLI**：容器启动时从 Google 官方清单服务拉最新版 `agy` 到专用卷，之后每 24 小时检查一次，有新版就原子替换卷里的二进制，不重启容器、不重新出镜像。只接入现有 `aiferry_default` 网络，不发布宿主机端口。专用卷 `agy-state` 保存全部账号的 CLI 登录状态、桥接令牌与官方 CLI 二进制。不要把这个卷挂载给其他服务，也不要把令牌输出到日志。仅在 Docker 网络中没有其他不受信服务时使用该配置。

**为什么不预置 CLI**：官方发版约一两周一次，旧版 CLI 的协议迟早和当前服务端不兼容，预置一份「兜底」只会把问题推迟到排查时才暴露，最终还是得更新。因此启动时拉不到官方清单就直接退出、由容器重启策略重试，不用旧版顶替。

**全局代理**：编排通过 `GLOBAL_PROXY` 一处配置，同时注入桥接容器的 `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY`。桥接下载官方 CLI 与渠道未配代理时的对话出网都读它；留空即直连。渠道配置了代理时仍以渠道为准（优先级：请求头 > 账号固定配置 > 全局代理）。`NO_PROXY` 必须排除内网段与回环，否则容器间通信（主服务连 MySQL/Redis/Casdoor）也会被代理拦下。

容器健康后（首次启动要先下载约 270MB 的 CLI，`start_period` 给了 180 秒）即可发起登录，见下一节。

查看当前生效的官方 CLI 版本（需桥接令牌，不消耗模型额度）：

```sh
docker exec --user 10001 aiferry-agy-bridge-1 sh -c 'curl -s -H "Authorization: Bearer $(cat /home/agy/state/bridge.token)" http://127.0.0.1:8091/v1/version'
```

## 登录（网页授权）

官方 CLI 不再自己输出授权地址，桥接因此内置了与官方客户端一致的 OAuth 2.0 + PKCE 流程（固定的回环回调与那组 scope）。授权在管理员自己的浏览器里完成，令牌只写进桥接卷，AiFerry 不保存 Google 凭据。

客户端凭据不写进源码：桥接从编排的环境变量 `AGY_BRIDGE_OAUTH_CLIENT_ID` 与 `AGY_BRIDGE_OAUTH_CLIENT_SECRET` 读取官方桌面客户端的 OAuth 客户端凭据，写法见 [.env.example](../.env.example)。这两项缺失时只影响「发起新登录」，已有登录态的对话与额度查询照常可用。

在 AiFerry 渠道 → 上游密钥 → 选择「平台登录」后：

1. 点「发起登录」，桥接生成授权地址；
2. 在浏览器打开该地址，用要接入的那个 Google 账号完成授权；
3. 浏览器会跳到 `http://localhost:51121/oauth-callback?code=...` 并提示「无法访问」——这是预期现象，回调落在管理员本机，不经过桥接容器；
4. 把地址栏里的完整地址（也可以只粘 `code` 参数）粘回对话框提交。

渠道下还没有任何桥接令牌时**不需要手工准备**：AiFerry 会自动生成一把随机令牌并写进该渠道的凭据（备注「网页授权自动创建」），桥接在开启 `AGY_BRIDGE_AUTO_ACCOUNT` 时对 `/v1/login/start` 收到的未知令牌自动登记账号，于是「新建渠道 → 点平台登录 → 网页授权」全程不需要管理员接触令牌。自动登记只对这一个端点开放，模型与额度端点仍然只认已存在的令牌。

桥接拿到授权码后换取 refresh/access token，按官方 CLI 的原生格式写入该账号 HOME 的 `.gemini/antigravity-cli/antigravity-oauth-token`（权限 0600），随后官方 CLI 就能直接用这份登录态。

为什么必须由人转交一次：这个客户端注册的回调固定是 `http://localhost:51121/oauth-callback`，Google 严格校验回环 host，既改不了也不能替换成服务器域名。授权会话 10 分钟有效、一次性使用，并且只能由发起它的那个桥接账号兑现（state 与账号绑定）。

需要的 scope 是 `cloud-platform`、`userinfo.email`、`userinfo.profile`、`cclog`、`experimentsandconfigs`；**不要加 `openid`**，否则 Google 会把这个客户端路由到会卡住的 firstparty/nativeapp 同意流程。

## 多账号（与 WorkBuddy 渠道一致）

一把桥接令牌对应一个 Google 账号。渠道下每把上游密钥填各自的桥接令牌，AiFerry 按自己的规则挑选密钥、轮换与并发查额度，桥接只负责「用这把令牌对应的账号调用官方 CLI」，自己不做账号选择。账号目录布局：

```
/home/agy/state/
├── bridge.token              默认账号（首启自动生成）
├── account-home/             默认账号的 HOME（登录状态）
├── cli/antigravity           官方 CLI 二进制（启动时自取，定期自更新）
├── cli/release.json          当前 CLI 版本记录
└── accounts/
    ├── alice/token           至少 32 字节的随机令牌
    ├── alice/home/           该账号的 HOME
    ├── alice/proxy           可选：该账号固定代理
    └── bob/token …
```

新增账号：在卷里建 `accounts/<名字>/`，写入 `token`（`openssl rand -hex 32`），然后以该账号的 HOME 登录一次：

```sh
docker exec -it --user 10001 --workdir /tmp -e HOME=/home/agy/state/accounts/alice/home aiferry-agy-bridge-1 /home/agy/state/cli/antigravity
```

令牌不足 32 字节的目录会让桥接拒绝启动。读取令牌填进 AiFerry（不要贴到对话或日志）：

```sh
docker exec --user 10001 aiferry-agy-bridge-1 cat /home/agy/state/accounts/alice/token
```

## 代理

渠道配置了代理时，AiFerry 转发请求会把**该密钥当前选中的代理地址**用内部请求头 `X-AiFerry-Proxy` 带给桥接（代理层失败顺延下一个代理时，头里是顺延后的地址）；桥接在令牌认证通过后读取它，并写进 `agy` 进程的 `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY`，官方 CLI 自己走该代理。未配代理的渠道不带此头，`agy` 直连；账号目录里的 `proxy` 文件是该账号的固定兜底。代理地址可能含账号密码，桥接不回显它，AiFerry 也不会把它带给非 Antigravity 渠道。

## 额度查询

渠道类型 **Google Gemini（Antigravity 订阅）** 自带额度适配器 `antigravity_usage`：桥接的 `GET /v1/quota` 调 `agy -p /usage --output-format stream-json`（不加 `--sandbox`——沙箱模式下 `/usage` 会卡到超时），把 `command_result` 里 Gemini 分组的 5 小时与每周两个窗口翻译成「已用百分比 + 重置时间」。多密钥渠道按密钥并发查询后合并（百分比平均、重置取最早），失败密钥列在明细里；单密钥查询单独出结果。结果缓存 1 分钟。

## 官方 CLI 的更新机制

桥接只认 Google 官方的清单服务 `antigravity-cli-auto-updater-*.us-central1.run.app`：读 `{version, url, sha512}` → 比对卷内版本 → 下载 → sha512 校验 → 解包 `antigravity` → 同卷 `rename` 原子替换。流程与官方 `install.sh` 一致，只是把目标目录换成卷。校验不通过拒绝安装；下载地址不在官方存储桶 `storage.googleapis.com/antigravity-public/` 下也拒绝，防止清单被篡改后把任意二进制写进卷。正在运行的旧进程继续用已加载的二进制，新请求直接拿到新版。

**升级官方 CLI 不需要重新构建桥接镜像**。桥接解析器对未知输出一律报错、不回退成猜测——新版 CLI 改了输出格式时，最坏情况是额度查询或对话明确报错，不会给出假数据；此时 `/v1/version` 能直接看到当前 CLI 版本，便于对照官方 CHANGELOG 定位。

## 服务器安装与验收

1. 桥接镜像自带下载与自更新机制，不再手动安装官方 `agy`。运行环境仍须满足：无提权能力的专用用户、独立 HOME，只保留此账号的 CLI 登录，不挂载业务文件、Docker socket 或其他凭据，不配置外部 MCP、插件或项目自定义功能。使用服务端已获授权的 Google 网络出口（直连不通时配 `AGY_BRIDGE_PROXY`）。由用户在这个运行环境内完成官方登录，不能迁移本机令牌；拒绝地区、资格时停止，不作绕过。
2. 容器首次启动会从官方清单下载最新 CLI；启动后先查 `/v1/version` 确认 CLI 版本已是官方当前版，再用目标模型发一次短消息确认本环境、该账号能收到 `OK`。模型列表存在不等于账号有权限调用。
3. 运行 `go build -o agy-bridge ./cmd/agy-bridge`。状态目录由 `AGY_BRIDGE_STATE_DIR` 指定（默认取 `AGY_BRIDGE_TOKEN_FILE` 所在目录），布局同上文。`AGY_BRIDGE_MODEL` 默认 `gemini-4-hydrogen-low`；`AGY_BRIDGE_LISTEN` 默认 `127.0.0.1:8091`，只接受回环监听。如果 AiFerry 在另一容器或服务器，通过仅 AiFerry 可达的私网、带 TLS 的反向代理连接回环服务，不开放公网入口；反向代理须关闭 SSE 缓冲并允许至少 110 秒的响应时间。
4. 在 AiFerry 新建 **Google Gemini（Antigravity 订阅）** 渠道，地址 `http://agy-bridge:8091/v1`，每把上游密钥填一个账号的桥接令牌（不是 Google 凭据）。发现模型后发一条纯文本流式请求，确认有文本、正常终止和用量；再点渠道的套餐额度，确认两个窗口都有数。旧的 `AIFERRY_GEMINI_OAUTH_*` 环境变量若仍残留，从有效部署配置清掉并保留其他配置。

这个桥接仅支持私用的多账号、单模型、文本和流式文本，每个账号串行处理。不会接受调用者提供的工具、附件或其他模型参数。CLI 每次在空工作目录运行，拒绝文件读写、shell、MCP 和浏览器工具权限，同时开启终端沙箱。不要把它作为共享服务或放在存在其他敏感文件的运行账号下。

本机可选端到端验证：设置 `AGY_BRIDGE_LIVE_TEST=1` 后执行 `go test -v ./cmd/agy-bridge`，需要本机官方 `agy` 已登录；常规 CI 不调用个人账号。
