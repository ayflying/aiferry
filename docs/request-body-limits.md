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

## 调整上限时的检查清单

1. 改端点上限 → 确认仍小于 `clientMaxBodySize`，跑 `go test ./internal/controller/relay/`。
2. 改 `clientMaxBodySize` → 同一测试会校验大小关系；同时确认容器内存能承受
   单请求缓冲（控制器层会把整个请求体读进内存，GoFrame 用的是流式限读、不会预分配）。
3. 上限**不是**越大越好：超过上游自身限制后，失败点会从网关挪到上游，
   那时才会有明确的上游报错与候选切换；网关侧上限过小则连切换机会都没有。
