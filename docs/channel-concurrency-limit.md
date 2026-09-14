# 渠道并发限制（按密钥隔离）

在渠道的「高级配置」里可以设置 **每把密钥的转发并发上限**（`concurrencyLimit`，0 表示不限制）。
额度按「渠道 × 密钥」独立计数，因此多把密钥互不占用：

> 一个渠道限制 15 并发、配了 3 把密钥 → 每把各 15 并发，该渠道合计 45 并发。

典型用途：上游按密钥限并发（或按密钥限速），配置后网关不会再把某把密钥压到超限；
同一渠道的其它密钥照常承接流量，不必为了单把密钥的限制而压低整个渠道。

## 使用方式

1. 渠道编辑 → 「高级配置」→ 「转发并发限制」；
2. 填入每把密钥的上限（1–1024），`0` 表示不限制；
3. 保存。渠道写操作会递增路由版本号，配置**立即生效**，不需要等缓存过期。

未配置（0）时行为与历史完全一致：不做任何并发判定、不排队、不额外占用内存。

## 额度判定与排队

一次请求进入转发阶段、选定密钥后先占用该密钥的一个额度，请求结束（流式请求等到流结束）
再归还。额度不足时按下面的顺序处理，全部发生在网关内部，客户端无感：

1. **先换空闲密钥**：同一渠道其它密钥还有空闲额度时直接改用那把密钥，不排队；
2. **全部占满则排队**：所有可用密钥都满额时等待，任意密钥释放额度立即唤醒继续转发；
3. **等待上限 60 秒**：窗口内仍没有空位，返回

   ```
   HTTP/1.1 429 Too Many Requests
   Retry-After: 5
   {"error":{"type":"rate_limit_exceeded","message":"Channel upstream key concurrency limit reached. Please retry shortly."}}
   ```

   该上限由 `keyConcurrencyWaitWindow` 定义，必须明显小于非流式上游超时（默认 600 秒）。

密钥挑选沿用既有逻辑（已绑定的优先、其余随机），因此即使不做排队，流量本来也会在多把
密钥间分散；并发限制只在此基础上剔除「已满」的密钥。

## 生效范围与失败语义

- **只作用于客户端转发链路**：`/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`、
  `/v1/images/generations`。
- 管理端的「测试渠道」「模型巡检」**不占用**额度：它们是管理员手动触发的低频探测，
  占用会让巡检在业务繁忙时拿不到空位而误报。
- **429 由网关本地产生，不参与渠道自动禁用评分**。默认 `DisableStatusCodes` 含 429，
  若把这次限流当成一次转发失败，健康渠道会被判为连续失败而自动禁用，因此该分支
  直接返回客户端错误、不写用量日志（只在服务端留下一条 warning，含渠道名与 ID）。
- 额度耗尽是**渠道级**判定：该渠道排队超时即返回 429，不会自动改投其它候选渠道。
  需要跨渠道兜底时，请给多渠道同时配置限额并依赖既有优先级与加权路由。

## 存储与接口

- 存储为渠道 `advanced_config` JSON 的新字段，**没有数据库迁移**：

  ```json
  { "backupBaseUrls": [], "blockStore": true, "concurrencyLimit": 15 }
  ```

- `POST/PUT /api/channels` 的 `advancedConfig` 对象增加 `concurrencyLimit`；
  取值必须是 0–1024 的整数，越界由 `ParseAdvancedConfig` 直接拒绝、不落库。
- `GET /api/channels` 返回的 `advancedConfig` 会带上该字段；旧渠道缺该字段时按 0（不限）处理。

## 已知限制

- **额度在单实例内计数**：网关以单容器运行时该值即渠道的真实并发；多副本部署时每个副本
  各自持有一份上限（合计为「副本数 × 配置值」），与请求防火墙的并发计数口径一致。
  需要精确的全局额度时，应改用 Redis 计数。
- 排队会**占用一个 HTTP 处理协程**（最多 60 秒），极端情况下并发排队会推高内存占用；
  这是「不丢请求」的代价，客户端断开（context 取消）会立即终止等待。
- **媒体链路暂未接入**：`/v1/audio/*`、`/v1/images/edits`、`/v1/video*` 不受该限制约束。
- 额度按密钥计数，**没有渠道级总量上限**：渠道总并发 = 可用密钥数 × 该值，密钥增删会
  直接改变渠道总并发。

## 实现位置

- `internal/logic/relay/concurrency.go`：`keySlots` 计数器与 `acquireKeySlot` 占用/排队逻辑。
- `internal/logic/relay/credential_retry.go`：单渠道重试循环内选密钥、占额度、归还额度。
- `internal/logic/relay/service.go`：`Candidate.ConcurrencyLimit` 与额度耗尽后的 429 返回。
- `internal/logic/relay/availability_error.go`、`internal/controller/relay/controller.go`：
  `ErrChannelConcurrencyExhausted` 到 429 + `Retry-After` 的映射。
- `internal/logic/channel/advanced.go`：`AdvancedConfig.ConcurrencyLimit` 的解析与校验。
- `frontend/src/components/ChannelAdvancedSettings.vue`：配置界面。
