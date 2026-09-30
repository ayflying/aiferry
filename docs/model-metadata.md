# 模型列表能力元数据

`GET /v1/models` 保留 OpenAI 兼容的 `object: "list"`、`data` 和每个模型的 `id`、`object`、`created`、`owned_by`。新增字段属于 AiFerry 扩展，不是 OpenAI 标准；客户端需要主动解析，不能保证 Octop 等第三方会自动显示对应标签。

## 扩展字段

以下字段位于每个 `data[]` 元素的 `metadata` 对象内（不是顶层字段）。

- `display_name`、`description`：显示名称和简介。
- `input_modalities`、`output_modalities`：`text`、`image`、`audio`、`video`、`file` 的列表。`null` 表示未知，`[]` 表示明确无，图片输入不等于图片生成。
- `context_length`、`max_output_tokens`：上下文和最大输出 Token 数，未知为 `null`。
- `capabilities.tools`、`capabilities.reasoning`、`capabilities.structured_output`：工具调用、推理、结构化输出，`true` 支持、`false` 不支持、`null` 未知。

不根据模型名称、价格或网关协议转换能力推测模型自身能力。本次不增加价格字段。OpenAI 标准模型列表不规定上下文和价格字段。

## 同步和手动覆盖

在渠道执行“获取模型列表”时，同步上游明确提供的元数据。只返回模型 ID 的上游会得到未知能力；已有渠道需重新获取模型列表，或手动补充。

管理员在模型管理页面点击能力编辑按钮，可以查看自动值和已保存生效值，按公开模型名配置手动覆盖。手动非 `null` 字段优先，手动 `null` 字段回退自动值；因此 `null` 不能强制抹除已知自动值。显式 `false` 和空列表 `[]` 则会覆盖自动值。清除覆盖恢复自动信息，不修改模型映射或价格。

同一公开模型对应多条可用路由时，对当前 API 密钥实际可见的候选进行保守聚合，避免把某个渠道的能力承诺给所有请求。手动配置的管理员需确保该公开模型的实际路由符合承诺。

管理接口（需要管理员 Session）：

- `GET /api/admin/model-metadata?publicName=模型名` 返回 `automatic`、`manual`、`effective`。
- `PUT /api/admin/model-metadata`，请求 `{ "publicName": "模型名", "metadata": { ... } }` 保存覆盖；`metadata: null` 清除覆盖。

## 存储和验证边界

元数据由数据库迁移新增表保存，上游信息与手动覆盖独立，刷新上游不会覆盖手工配置。上线前由应用正常迁移流程应用新增迁移，数据库必须具备迁移权限。

本机基础检查覆盖解析、三态值、聚合、覆盖及 JSON 兼容性；正式构建由 CI 完成。测试通过不代表生产已部署，也不能代替第三方客户端实际兼容性验证。
