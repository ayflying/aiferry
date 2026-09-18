# 请求体上限与「Unable to read request body」

## 现象

客户端（长上下文、带图对话）偶发被网关直接拒绝：

```json
{"error":{"message":"Unable to read request body","type":"invalid_request_error"}}
```

特征：

- 请求**没有进入 relay 层**：`usage_logs` 无记录、原始报文目录无文件、渠道切换完全没发生
  （客户端看到的是一次「当场中断」，不会重试也不会换渠道）。
- 与模型、渠道、余额无关，同一把密钥小请求一切正常。

## 成因

两层限制叠加，8MiB 是那条看不见的红线：

1. **GoFrame 默认 `server.clientMaxBodySize = 8MiB`**。`ghttp.Server.ServeHTTP` 会无条件用
   `http.MaxBytesReader(w, r.Body, s.config.ClientMaxBodySize)` 包住请求体；超过后底层 `Read`
   返回 `*http.MaxBytesError`，控制器层读到错误就回 400。项目此前从未在
   `manifest/config/config.yaml` 里覆盖该配置。
2. **控制器层的缓冲上限比它大**：`/chat/completions` 与 `/images/*` 按 32MiB、`/audio/*` 24MiB、
   `/video*` 64MiB 缓冲，全部高于 GoFrame 的 8MiB —— 也就是说这些上限在 0.5.137 之前
   **从未真正生效**，请求先被 GoFrame 拦掉了。
3. 另外 `io.LimitReader(r.Body, limit+1)` 只会**静默截断**、不报错。一旦哪天截断先发生，
   请求体会变成非法 JSON，把「太大」伪装成下游难以定位的解析错误。

## 上限体系（0.5.137 起）

| 层 | 位置 | 上限 |
| --- | --- | --- |
| GoFrame 网关 | `manifest/config/config.yaml: server.clientMaxBodySize` | 96MiB |
| chat / responses / embeddings / images | `internal/controller/relay/body.go` `maxChatRequestBodyLimit` / `maxImagesRequestBodyLimit` | 32MiB |
| audio | `maxAudioRequestBodyLimit` | 24MiB |
| video | `maxVideoRequestBodyLimit` | 64MiB |

**约束：GoFrame 的上限必须严格大于每个端点上限**，否则端点配置形同虚设。
`body_test.go: TestGatewayBodyCapCoversEndpointLimits` 会直接解析 `config.yaml` 断言这一点，
改配置忘了同步就会在单测里失败。

## 失败响应语义

控制器层统一走 `readRequestBody`，按原因分类（此前一律 400，且不留日志）：

| 情形 | 状态码 | 文案要点 |
| --- | --- | --- |
| 超过 GoFrame 网关上限 | 413 | 报出真实上限（`http.MaxBytesError.Limit`） |
| 超过端点缓冲上限 | 413 | 报出该端点上限 |
| 客户端中途断开 / 上传超时 | 400 | 注明「未完整收到，请重试」 |
| 其它未知读取错误 | 400 | 保留原 `Unable to read request body`，不改变既有兼容行为 |

每次失败都会记一条 WARN 日志，这是该类问题唯一的排查线索（不落库、不落报文）：

```text
read request body failed: endpoint=/chat/completions client=<ip> contentType="application/json" \
  contentLength=8600087 readBytes=8388608 limit=100663296 error=http: request body too large
```

## 排查与验证

生产探针（不消耗上游额度）：用**不存在的模型名**打生产网关，请求只走到路由层。
小 body 应返回路由层的错误（`All eligible channels are temporarily unavailable`），
大 body 若返回 `Unable to read request body` 即命中本问题：

```text
8,387,967 字节 → 503（读取成功）        8,389,967 字节 → 400（读取失败）
```

生产报文体积分布可作为「离红线有多近」的证据（报文截断上限同为 8MiB）：
`/app/data/relay-payloads` 下按文件大小分桶统计，若出现大量 7–8MB 的文件，
说明请求体长期贴着上限运行。

## 这些超大请求到底是什么

2026-09-18 对生产 `/app/data/relay-payloads`（1071 份）做了画像，结论是**它们不是文件上传，
而是 agent 类客户端的长会话对话请求**：

| 体积档 | 份数 |
| --- | --- |
| > 8 MB | 23 |
| 6–8 MB | 204 |
| 4–6 MB | 221 |
| 1–4 MB | 339 |
| ≤ 1 MB | 285 |

最大一份 8,394,359 字节（`afreq_b9b44d2ad111ae5d8592b155`）：

- **94.1% 是内联 base64 图片**（7,895,026 字节，30 张 JPEG）；
- 去掉所有 `data:image/...` 后只剩 499,333 字节，其中 system 提示词仅 2,721 字节；
- 消息结构：1 条 system、31 条 user、97 条 assistant（带 `tool_calls`）、97 条 tool 结果；
  最大的样本达到 112 条 user / 213 条 assistant / 213 条 tool / 109 张 JPEG。

即请求体大小由**会话历史长度 × 历史截图数量**决定：客户端每一轮都把全部历史（每张截图以
base64 重新内联、每次工具调用的完整输出）重放一遍，会话越长就越贴近上限。

渠道侧没有异常：超 6MB 的样本来自 `adesk`（GLM Coding Plan）与 `opencodeGo`，模型
`glm-5.3-flash`，`httpStatus=200`、`attempts=1`，都属于**正常成功**的请求。代价体现在延迟：
这些请求 `first_token_ms` 为 24.4–36.9 秒；近 24 小时同模型下 `adesk` 平均首字 16.1 秒，
而「智谱」渠道仅 7.4 秒。

体积画像的取法（容器内单遍扫描，不下载报文到本地）：

```sh
cd /app/data/relay-payloads
BIG=$(ls -S | head -1)
awk '{n=length($0); t=$0; gsub(/data:image\/[^"]*/,"",t);
  printf "总=%d 去图后=%d 图片占比=%.1f%%\n", n, length(t), (n-length(t))*100/n}' "$BIG"
grep -o 'iVBORw0KGgo' "$BIG" | wc -l   # PNG 头计数
grep -o '/9j/4AAQ' "$BIG" | wc -l      # JPEG 头计数
```

遍历上千份文件时逐个 `grep`（每份 8MB）会超时，优先 `ls -S | head` 取头部样本再细看。

## 调整上限时的检查清单

1. 改端点上限 → 确认仍小于 `clientMaxBodySize`，跑 `go test ./internal/controller/relay/`。
2. 改 `clientMaxBodySize` → 同一测试会校验大小关系；同时确认容器内存能承受
   单请求缓冲（控制器层会把整个请求体读进内存，GoFrame 用的是流式限读、不会预分配）。
3. 上限**不是**越大越好：超过上游自身限制后，失败点会从网关挪到上游，
   那时才会有明确的上游报错与候选切换；网关侧上限过小则连切换机会都没有。
