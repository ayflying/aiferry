# 渠道组织 / 项目标识

渠道表单里的「组织 ID」「项目 ID」是 **OpenAI 官方渠道专有**的身份标识，用于把上游用量归属到指定组织与项目。它们**不是废字段**，只是对其它渠道类型没有意义。

## 两个字段的实际作用

| 用途 | 实现位置 | 行为 |
| --- | --- | --- |
| 转发鉴权头 | `internal/logic/channel/upstream_auth.go` | 字段非空时给上游请求写入 `OpenAI-Organization` / `OpenAI-Project` |
| 成本查询 | `internal/logic/channel/cost_upstream.go` | 字段非空时作为 `project_ids` 参数传给 `/organization/costs` |

两者落库在 `channels.organization_id` / `project_id`，随候选渠道进入路由缓存，因此修改渠道后立即生效；管理端「测试渠道」与模型发现/同步链路共用同一套注入实现。

## 表单展示条件

字段只在渠道类型声明的成本适配器为 `openai_costs` 时展示。该适配器全库只由 OpenAI 官方类型声明（见 `manifest/builtins.json`），语义与上面两条用途完全重合，因此直接用它作判据，无需新增渠道类型配置字段。

判据实现在 `frontend/src/lib/channelForm.ts` 的 `supportsOrganizationIdentity()`。其余渠道类型上游不认这两个头，填了没有任何效果，所以表单不再展示。

**注意：只是不展示，没有删除。** 后端注入逻辑、数据库列、成本查询参数全部保留，OpenAI 官方渠道的用量归属与成本对账不受影响。

## 保存时清空

保存渠道时，若当前类型不支持组织 / 项目标识，两个字段会被置空后提交。这样不会留下「表单里看不见、请求里却仍在发」的隐形配置 —— 比如把 OpenAI 渠道改成 DeepSeek 后，`OpenAI-Organization` 头不该继续发出去。

相应地，切走类型会清掉原有的值，切回 OpenAI 类型需要重新填写。

## 已知限制

- 跨源下载视频文件时（视频 URL 与渠道根地址不同源），这两处标识与密钥、代理会被一起清空，改为直连下载，不向上游发组织 / 项目头（`internal/logic/relay/video.go`）
- 头名固定为 `OpenAI-Organization` / `OpenAI-Project`，不做自定义配置
