# AiFerry 私用 Antigravity 文本渠道

自建 Google OAuth 已停用。桥接只调用已安装、未经修改的官方 `agy`，由 `agy` 自己管理账号授权；不复制 Google 令牌，不调用内部模型发现接口。实现参考 [dsh-agy-link](https://github.com/amlyczz/dsh-agy-link) 的命令行模式及 [Google 无头模式文档](https://antigravity.google/docs/cli/headless/)。

## Arkade 独立容器编排

用 [compose.yml](../deploy/agy-bridge/compose.yml) 新建独立编排 `agy-bridge`。镜像由 CI 发布，内含固定版本的官方 CLI；只接入现有 `aiferry_default` 网络，不发布宿主机端口、不重启 AiFerry。专用卷只保存该 CLI 的登录状态及启动时生成的桥接密钥。不要把这个卷挂载给其他服务，也不要把密钥输出到日志。仅在 Docker 网络中没有其他不受信服务时使用该配置。

容器起来后，使用支持交互的终端，在该容器内以 UID `10001` 运行官方 `agy` 完成登录（Arkade 的逐命令控制台不等于交互终端）。先验证官方 CLI 能实际调用模型。AiFerry 的 OpenAI 渠道地址为 `http://agy-bridge:8091/v1`；只在渠道设置中填写专用卷里的桥接密钥，不填 Google 凭据。最后从 AiFerry 发一次流式请求验收。

## 服务器安装与验收

1. 在隔离的服务器运行环境安装官方 `agy`（已在 1.5.18 验证），使用无提权能力的专用用户、独立 HOME，只保留此账号的 CLI 登录，不挂载业务文件、Docker socket 或其他凭据，不配置外部 MCP、插件或项目自定义功能。使用服务端已获授权的 Google 网络出口。由用户在这个运行环境内完成官方登录，不能迁移本机令牌；拒绝地区、资格时停止，不作绕过。
2. 运行 `agy models`，然后用目标模型发一次短消息，例如 `agy --model gemini-4-hydrogen-low -p 'Reply with exactly OK.' --sandbox --disable-slash-commands`。确认本环境、该账号能收到 `OK`。模型列表存在不等于账号有权限调用。
3. 运行 `go build -o agy-bridge ./cmd/agy-bridge`。将至少 32 字节的随机桥接密钥放到仅服务用户可读的专用文件，以 `AGY_BRIDGE_TOKEN_FILE` 指定路径，勿提交到 Git。`AGY_BRIDGE_MODEL` 默认 `gemini-4-hydrogen-low`；`AGY_BRIDGE_LISTEN` 默认 `127.0.0.1:8091`，只接受回环监听。如果 AiFerry 在另一容器或服务器，通过仅 AiFerry 可达的私网、带 TLS 的反向代理连接回环服务，不开放公网入口；反向代理须关闭 SSE 缓冲并允许至少 110 秒的响应时间。
4. 在 AiFerry 新建 **OpenAI** 渠道，将上游地址设为桥接地址加 `/v1`，渠道密钥填桥接密钥（不是 Google 凭据）。发现模型后，用 `/v1/chat/completions` 发一条纯文本流式请求，确认有文本、正常终止和用量。已删除的 `gemini_subscription` 渠道不可继续使用；旧线上环境文件若仍有 `AIFERRY_GEMINI_OAUTH_CLIENT_ID`、`AIFERRY_GEMINI_OAUTH_CLIENT_SECRET`、`AIFERRY_GEMINI_OAUTH_REDIRECT_URI`，从有效部署配置清掉这三个键并保留其他配置。

这个桥接仅支持私用的单账号、单模型、文本和流式文本，串行处理。不会接受调用者提供的工具、附件或其他模型参数。CLI 每次在空工作目录运行，拒绝文件读写、shell、MCP 和浏览器工具权限，同时开启终端沙箱。不要把它作为共享服务或放在存在其他敏感文件的运行账号下。

本机可选端到端验证：设置 `AGY_BRIDGE_LIVE_TEST=1` 后执行 `go test -v ./cmd/agy-bridge`，需要本机官方 `agy` 已登录；常规 CI 不调用个人账号。
