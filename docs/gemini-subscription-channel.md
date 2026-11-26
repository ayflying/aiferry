# Gemini 订阅渠道（实验）

该渠道与普通 Gemini API Key 渠道独立。一个渠道可以添加多个 Google 账号，每个账号绑定自己的项目并独立同步模型。支持 Chat Completions 文本对话（含流式），不支持工具、多模态或订阅余额查询。

## 接入前确认

该适配依赖 Google 未公开承诺稳定性的 Cloud Code Assist 端点。必须使用操作者自己的 OAuth 客户端和获得对应授权的 Google Cloud 项目；不要复用第三方应用的客户端身份。Google 可能拒绝自建应用访问相关端点，个人免费账号不代表具备 API 权限，亦不能证明付费订阅额度被使用。请先核对 [Google Antigravity 服务条款](https://antigravity.google/terms/)，未获授权的凭据或服务不得经此转发。

## 配置与验证

1. 在 AiFerry 的主节点配置 `AIFERRY_GEMINI_OAUTH_CLIENT_ID`、`AIFERRY_GEMINI_OAUTH_CLIENT_SECRET` 与 `AIFERRY_GEMINI_OAUTH_REDIRECT_URI`。回调地址必须是 AiFerry 对外 HTTPS 地址加 `/api/oauth/gemini-subscription/callback`，与 Google 控制台登记值完全一致。不要把密钥写入仓库；所有需要续期或转发的节点应使用同一 OAuth 客户端配置。更换客户端配置后应重新授权。
2. 反向代理访问日志不得记录回调查询字符串，浏览器回调含一次性授权码与状态值。
3. 新建「Gemini 订阅（实验）」渠道，只在渠道上填写代理。代理需从 AiFerry 进程所在环境可达；容器内 `127.0.0.1` 不是 Windows 宿主机。换票、读取项目列表、续期、模型同步及转发都使用渠道代理，不回退直连。
4. 在「上游密钥 → 添加 Google 账号」由账号所有者亲自完成 Google 授权，然后从下拉框选择已有的 Google Cloud 项目并确认绑定。未授权读取项目列表、列表为空或读取失败时，可手动填写该账号有权使用的项目 ID；如果取消了整个 Google 授权，可用手填的 ID 重新发起授权，不再请求项目列表权限。AiFerry 不创建项目，也不绑定账单。项目出现在列表里不代表上游可用：为每个账号分别同步模型，只有同步结果包含目标模型的账号才可转发。从 `0.7.27` 升级的旧账号沿用原渠道项目 ID，需重新同步一次模型才能参与转发。
5. 如果旧授权失效，从该账号行选择「重新登录」。只有同一账号授权成功才更新原凭据，项目 ID、备注、启停状态和绑定保留；不同账号请走「添加账号」。账号密码、验证码和令牌不需要发给运维人员。
6. 选择同步结果中的文本模型发送短消息验证流式回复；随后在测试环境阻断代理，确认不会直连。

参考实现：[dsh-gemini-pool](https://github.com/qikairo7/dsh-gemini-pool/tree/e86a1eafae729f8f007ef8a74a44a631cff4bc40)。本实现不安装该插件，也不沿用其 OAuth 客户端配置。
