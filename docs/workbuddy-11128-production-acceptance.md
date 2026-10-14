# WorkBuddy 11128 生产验收（2026-10-14）

## 结论

已发布 0.6.6，提交 d80d7ad0a8f45a1b4356be28b8053ff707a8e164。已验证的根因是通用 HTTP 库 User-Agent 被上游渠道校验拦截；网关仅对 WorkBuddy 使用自身标识规范化，不冒充官方客户端。

## 修复前生产证据

- 真实失败：usage_logs 213593（hy3）、213592（space-bunny）、213591（step-5-preview），渠道35/凭据52，HTTP400，上游11128。Chat直通，无协议转换。
- 同请求体、密钥、渠道，仅替换UA：合成请求 python-requests 两轮400（213740/213745）；aiferry标识两轮200（213742/213750）。curl与DeepSeekHarness标识也均成功。
- 真实失败报文重放（保留历史与工具，输出预算限制64）：python-requests 两轮400（213925/213937），aiferry标识两轮200（213930/213941），输入40634 token。排除该已测报文的内容、令牌有效性及出口整体拒绝作为原因；不泛化到全部风控。

## 发布与版本

- CI：https://github.com/ayflying/aiferry/actions/runs/38549058644 ，container任务成功。
- 镜像 ghcr.io/ayflying/aiferry:0.6.6；运行镜像ID sha256:497c4171115f90d713bce85ffb56690a82e01f5ccab4891364fad1d0a1c08fab。
- 运行容器56fdad0c7f45ba65c7e93e9628e4597d73c8d3b1a566986d2d62a91887b7cf2c，running/healthy，38517→8080。
- 公网https://aiferry.yunloli.cn/healthz、容器127.0.0.1:8080、宿主网络127.0.0.1:38517均报告version=0.6.6，revision=d80d7ad0a8f45a1b4356be28b8053ff707a8e164，status=ok；镜像revision标签一致。
- Portainer stack22/endpoint10部署，Env前后内存哈希一致，Compose只替换镜像引用，由latest固定到0.6.6。
- 本机及干净发布树go build/go test全通过；Vue类型检查和155项前端测试通过。仅属发布基础门禁，不替代生产证据。

## 上线后生产回归

时间窗：2026-10-14 18:31:19—18:34:21（Asia/Shanghai）。

|模型/形态|样本|结果|生产用量日志|
|---|---:|---|---|
|hy3 合成，3种UA×2种流式形态|6|全200、OK、usage|214222—214227|
|space-bunny，同上|6|全200、OK、usage|214228—214233|
|step-5-preview，同上|6|全200、OK、usage|214234/214235/214236/214237/214238/214241|
|hy3真实DSH历史与工具重放，两轮|10|全200、usage，8条SSE完整DONE，2条非流式tool_calls|214252/214255/214256/214257/214258/214260/214261/214265/214266/214267|

UA分别为python-requests/2.32.3、Python-urllib/3.12、DeepSeekHarness/1.0。28条记录均channel_id=35、attempts=1、input/output tokens非空。真实工具输出非流式记录214258/214267均finish_reason=tool_calls、函数read_image；仅核对模型返回，不执行工具。所有探针均完整读完，非仅检测首个token。

## 策略与清理

渠道35仍status=1、priority=0、weight=1、advanced_config与取证基线一致；凭据52仍status=1。未写渠道/路由/余额配置，未创建或更改API key、未补余额，测试按既有计费策略记账。hy3/space-bunny记录estimated_cost=0，step-5-preview按既有规则有成本，不据此修改定价。

临时宿主健康容器c84264e48c61c3b4d7206e11a1b2097d756827f5026138441c07f2fbb9ca19a1启动失败后已删除，生产容器查询确认临时标签无残留。宿主健康改用现有host网络容器只读请求成功。最近200行运行日志ERROR/FATAL/panic/nil pointer计数0。

本轮临时脚本、独立索引、干净源码验证目录在完成后清理；不删除其他任务已有取证文件。生产历史用量与正常报文保留用于审计。

## 验收边界

生产合成及真实失败报文重放已通过，不声称已操作DSH界面跑完整工具循环，也不保证上游未来风控不变化。本次解决的是有生产证据的UA兼容问题。
