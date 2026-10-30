# Rerank 重排中转

## 调用

入口：`POST /v1/rerank`，使用 AiFerry API Key 的 `Authorization: Bearer ...` 鉴权。
例如先取回两份候选文档，再让重排模型判断哪份与查询更相关：

```json
{
  "model": "公开模型名",
  "query": "湖北省的省会是哪里？",
  "documents": ["上海是直辖市。", "武汉是湖北省省会。"],
  "top_n": 1,
  "return_documents": true
}
```

- `query` 为非空字符串；`documents` 为非空数组，支持字符串及上游支持的对象文档。
- `top_n` 可省略，提供时须为正整数；`stream` 省略或为 false，不支持流式。
- 默认还保留 `max_chunks_per_doc`、`rank_fields`、`max_tokens_per_doc`、`truncate_input`、`truncation`、`overlap_tokens`。各上游支持的选项不同，请只提交其支持的字段；供应商额外参数可使用已有请求体透传配置。
- 公共模型名映射为上游模型名，复用现有鉴权、渠道选择、密钥并发限制、重试、敏感词检查和用量记录。
- `results` 中的 `index`、`relevance_score`、文档及供应商元数据原样返回，不转换为聊天响应，也不注入聊天缓存键。

## 渠道配置与测试

渠道根地址为 `https://provider.example/v1` 时，默认上游地址为 `https://provider.example/v1/rerank`。
可以在渠道类型 JSON 中覆盖路径及鉴权，例如 Cohere 风格接口：

```json
{
  "endpoints": {
    "rerank": {
      "method": "POST",
      "path": "https://api.cohere.com/v2/rerank",
      "requestBody": "json",
      "supportsStream": false,
      "authType": "channel_key",
      "headerName": "Authorization",
      "headerPrefix": "Bearer "
    }
  },
  "protocol": { "rerankModels": ["rerank-*", "自定义上游模型名"] }
}
```

管理端模型测试可手选 **Rerank 重排**；自动模式识别模型名中的 `rerank`，或命中渠道类型 `protocol.rerankModels` 名单（精确名/前缀通配）。测试始终非流式。自定义路径不改变用量记录及价格规则的端点分类 `/rerank`。

## 用量与计费边界

- 常规 `usage.input_tokens` / `prompt_tokens` 等字段沿用现有解析。
- Jina 风格只提供 `usage.total_tokens` 时，作为输入 token，输出为 0。
- 支持 Cohere `meta.tokens.input_tokens` / `output_tokens`。
- `meta.billed_units.search_units` **不是 token**，不换算、不伪造；这类没有 token 的上游应配置已有的“按请求”价格，每次成功请求计一次，不按 search_units 倍乘。
- 已定价模型缺少其计费必需用量时，仍按原有规则拒绝成功扣费，不使用文本长度估算 token。

该支持面向标准 JSON rerank 接口；不包含 Bedrock、云厂商签名鉴权或专有嵌套请求格式转换。测试使用本地模拟上游，不能替代具体供应商和生产配置验收。
