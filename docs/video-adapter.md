# 视频生成适配层设计

> 适用版本：自 2026-09 起（视频适配器声明机制上线后）
> 关联代码：`internal/logic/channeltype/service.go`（`VideoConfig`）、`internal/logic/channel/video_adapter.go`（`VideoAdapterFor`）、`internal/logic/relay/video.go`（`videoAdapter`）

## 1. 背景与目标

各视频生成厂商的 API 形态差异极大：创建端点、请求载荷、任务查询、成品下载各有各的协议。AiFerry 作为 LLM 网关，目标是：

1. **业务方只对接一种 API**——统一以 OpenAI 视频接口为对外形态（`POST /v1/videos` 创建、`GET /v1/videos/{id}` 查询、`GET /v1/videos/{id}/content` 下载），同时保留 legacy `/v1/video/generations` 端点兼容老用户。
2. **新增厂商只改配置 + 一个适配器**——渠道类型通过 `config.video.adapter` 声明协议形态，网关在转发时自动翻译，业务方无感知。
3. **协议差异收敛到一处**——避免在 relay 层散落 `channelType == "xxx"` 硬编码分支。

## 2. 架构

```
业务方（OpenAI 形态请求）
        │
        ▼
┌─────────────────────────────────────────────┐
│  relay/video.go                             │
│                                             │
│  videoAdapterFor(candidate)                 │
│      └─ 从渠道类型配置解析 adapter（带缓存）  │
│                                             │
│  videoAdapter（按渠道解析一次，随请求分发）   │
│   ├─ createURL()      创建任务端点           │
│   ├─ retrieveURL()    任务查询端点           │
│   ├─ prepareBody()    请求体翻译             │
│   ├─ queryTaskOnUpstream()   查询+资源改写   │
│   └─ downloadAdapterVideo()  成品下载        │
└─────────────────────────────────────────────┘
        │ 翻译后的私有协议请求
        ▼
   MiniMax / 火山方舟 / OpenAI 兼容上游
```

关键点：

- **解析时机**：`videoAdapterFor` 在每次转发前按渠道类型解析，实现复用音频适配器（`AudioAdapterFor`）的 5 分钟缓存；解析失败回退 `openai` 并记告警，与音频行为一致。
- **路由绑定**：创建成功后把 `videoTaskRoute`（含 mode、candidate、resourceUrl）存 Redis（TTL 24h），后续查询/下载按 taskID 取回，无需业务方重复传渠道信息。
- **资源改写**：MiniMax / 方舟查询响应里的成品 URL 会被改写为网关内容端点（`/v1/videos/{id}/content`），避免上游 CDN 地址直接暴露；真实下载地址缓存到路由记录。
- **跨域下载**：成品文件与渠道不同源时自动去掉渠道代理/凭证，走直连下载（`DirectHTTP`）。

## 3. 内置视频适配器

| adapter | 渠道类型 | 创建任务 | 查询任务 | 成品下载 | 请求体翻译 |
|---|---|---|---|---|---|
| `openai`（默认） | OpenAI 及所有兼容网关 | `POST {base}/videos`（OpenAI 形态）<br>`POST {base}/video/generations`（legacy） | `GET {base}/videos/{id}` 或 `GET {base}/video/generations/{id}` | `GET .../{id}/content` | 原样透传 |
| `minimax` | MiniMax（`minimax`） | `POST {base}/v2/video_generation` | `GET {base}/v2/query/video_generation/{id}` | 查询响应 `url` → 网关代理下载 | `prompt` → `content: [{type:"text",text:...}]` |
| `volcengine_ark` | 火山方舟视频（`volcengine_ark_video`） | `POST {base}/contents/generations/tasks` | `GET {base}/contents/generations/tasks/{id}` | 响应 `content.video_url` → 网关代理下载 | 原样透传（方舟本身接受 prompt 字段） |

MiniMax / 方舟没有独立的内容下载端点：成品地址藏在查询响应里，由 `queryTaskOnUpstream` 解析（MiniMax 走 `url` / `task.content.url` / `data.url` 路径，方舟走 `content.video_url`），改写为网关端点后缓存进路由。

## 4. 对外 API 形态（OpenAI 兼容）

### 4.1 创建视频

```bash
# OpenAI 形态（推荐）
curl -X POST https://<网关>/v1/videos \
  -H "Authorization: Bearer <AiFerry Key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"<模型名>","prompt":"一只船划过湖面","seconds":5}'

# Legacy 形态（兼容）
POST /v1/video/generations   # 同样载荷，也支持 multipart/form-data（model 字段在表单里）
```

网关行为：按模型路由 → 选渠道 → 按渠道适配器翻译请求体与端点 → 上游创建 → 提取 `task_id` / `id` 存路由 → 返回上游响应。

### 4.2 查询任务

```bash
GET /v1/videos/{video_id}          # OpenAI 形态
GET /v1/video/generations/{id}     # legacy
```

返回上游原始查询响应（MiniMax / 方舟的资源 URL 已改写为网关端点）。

### 4.3 下载成品

```bash
GET /v1/videos/{video_id}/content          # OpenAI 形态
GET /v1/video/generations/{id}/content     # legacy
```

网关优先使用路由里缓存的资源地址，缺失时先查一次任务再解析；成品尚未生成时返回错误提示。

## 5. 新增一个厂商适配器

以新增"快手可灵（kling）"为例：

1. **channeltype 定义协议常量**（`internal/logic/channeltype/service.go`）：
   ```go
   const VideoAdapterKling = "kling"
   ```
   在 `config.go` 的 `switch config.Video.Adapter` 里登记取值。

2. **relay/video.go 实现协议差异**：
   - `videoAdapter.createURL` / `retrieveURL` 加分支；
   - 如需请求体翻译，在 `prepareBody` 加分支；
   - 如成品地址藏于查询响应，在 `queryTaskOnUpstream` 加分支（新增 `klingVideoResponseURL` 等解析函数）。

3. **builtins.json 声明内置渠道类型**（如为内置）：
   ```json
   {
     "id": 9000000000000022,
     "name": "可灵视频",
     "code": "kling_video",
     "config": {
       "baseUrl": "https://api.klingai.com",
       "models": { "...": "..." },
       "costs": { "adapter": "none" },
       "pricing": { "adapter": "none" },
       "video": { "adapter": "kling" }
     }
   }
   ```
   注意：新内置类型 ID 取当前最大值 +1（查看文件内 `"id": 9000` 现状），且同步更新 `internal/config/builtins_test.go` 的数量断言与映射表。

4. **前端类型**：`frontend/src/api/types/channel.ts` 的 `ChannelTypeVideoConfig.adapter` 联合类型加新值。

5. **测试**：
   - `video_test.go` 的 `TestVideoAdapterURLsPerProtocol` 加一行用例（四种 URL）；
   - 有请求体翻译/资源解析时补对应单测；
   - `go test ./internal/logic/... ./internal/config/` 全绿。

## 6. 常见厂商差异速查（供后续适配参考）

| 厂商 | 创建任务 | 查询任务 | 载荷特点 | 备注 |
|---|---|---|---|---|
| OpenAI Sora | `POST /v1/videos` | `GET /v1/videos/{id}` | JSON，`prompt`/`seconds` | 原生形态 |
| MiniMax | `POST /v2/video_generation` | `GET /v2/query/video_generation/{id}` | `content` 数组，`model` 保留 | 已接入 ✅ |
| 火山方舟 Seedance | `POST /contents/generations/tasks` | `GET /contents/generations/tasks/{id}` | JSON，`content` 数组 | 已接入 ✅ |
| 可灵 Kling | `POST /v1/videos/text2video` | `GET /v1/videos/text2video/{id}` | JSON + 签名头（JWT） | 鉴权需签名适配 |
| 智谱 CogVideoX | `POST /paas/v4/videos/generations` | `GET /paas/v4/videos/generations/{id}`（异步） | JSON | 部分模型同步返回 |
| 阿里百炼 Wanx | `POST /services/aigc/video-generation/video-synthesis` | `GET /tasks/{task_id}` | `X-DashScope-Async: enable` 头 | 任务型 |
| Vidu | `POST /ent/v2/img2video` 等按模式分端点 | `GET /ent/v2/tasks/{id}/creations` | JSON | 端点按生成模式拆分 |
| Runway | `POST /v1/image_to_video` 等 | `GET /v1/tasks/{id}` | 按模式分端点 | REST 风格 |

> 适配优先级：先接业务实际用到的厂商；接入前以该厂商当前官方文档为准核对端点与字段（上表为设计参考，字段名可能随版本变化）。

## 7. 注意事项

- **视频适配器与音频适配器相互独立**：一个渠道类型可以同时声明 `audio.adapter` 与 `video.adapter`，互不影响。
- **自定义渠道类型**同样支持 `config.video.adapter`（数据库配置走同一 `ParseConfig` 校验，未知 adapter 会被拒绝）。
- **超时**：视频上游调用统一 90 秒（`callVideoUpstream` 内）；请求/响应体上限 64 MiB。
- **鉴权**：上游统一 `Authorization: Bearer <渠道密钥>`；方舟等需要 AK/SK 签名的渠道先经渠道密钥转换层处理（沿用现有凭证机制）。
- **资源 URL 安全**：改写后的端点仅暴露网关路径；下载成品前校验资源 URL 必须为 HTTP/HTTPS，且跨域时剥离渠道敏感配置。
