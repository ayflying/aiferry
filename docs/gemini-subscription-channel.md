# Gemini 订阅渠道（实验）

该渠道与普通 Gemini API Key 渠道独立。首版面向单账号、按账号发现模型以及 Chat Completions 文本对话（支持流式），不支持工具、多模态、订阅余额查询或多账号池。

## 接入前确认

该适配依赖 Google 未公开承诺稳定性的 Cloud Code Assist 端点。必须使用操作者自己的 OAuth 客户端和获得对应授权的 Google Cloud 项目；不要复用第三方应用的客户端身份。Google 可能拒绝自建应用访问相关端点，个人免费账号不代表具备 API 权限，亦不能证明付费订阅额度被使用。请先核对 [Google Antigravity 服务条款](https://antigravity.google/terms/)，未获授权的凭据或服务不得经此转发。

## 配置与验证

1. 在 AiFerry 的主节点配置 `AIFERRY_GEMINI_OAUTH_CLIENT_ID`、`AIFERRY_GEMINI_OAUTH_CLIENT_SECRET` 与 `AIFERRY_GEMINI_OAUTH_REDIRECT_URI`。回调地址必须是 AiFerry 对外 HTTPS 地址加 `/api/oauth/gemini-subscription/callback`，与 Google 控制台登记值完全一致。不要把密钥写入仓库；所有需要续期或转发的节点应使用同一 OAuth 客户端配置。更换客户端配置后应重新授权。
2. 反向代理访问日志不得记录回调查询字符串，浏览器回调含一次性授权码与状态值。
3. 新建「Gemini 订阅（实验）」渠道，填写已授权的 Google Cloud 项目 ID 与渠道代理。代理需从 AiFerry 进程所在环境可达；容器内 `127.0.0.1` 不是 Windows 宿主机。换票、续期、模型同步及转发都使用渠道代理，不回退直连。
4. 在「上游密钥」入口由账号所有者亲自完成授权。同步模型，选一个实际返回的文本模型，发送短消息验证流式回复；随后在测试环境阻断代理确认不会直连。账号密码、验证码和令牌不需要发给运维人员。

参考实现：[dsh-gemini-pool](https://github.com/qikairo7/dsh-gemini-pool/tree/e86a1eafae729f8f007ef8a74a44a631cff4bc40)。本实现不安装该插件，也不沿用其 OAuth 客户端配置。
