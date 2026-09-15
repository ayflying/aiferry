# 渠道类型 / 价格源的地址字段解析规则

渠道类型配置（`channel_types.config`）与价格源配置（`price_sources.config`）里有多个
「路径」类字段：`models.path`、`costs.path`、`pricing.path`、`quota.path`、`endpoints.*.path`。
它们都接受两种写法：

| 写法 | 示例 | 解析结果 |
| --- | --- | --- |
| 相对路径（以 `/` 开头） | `/models` | 按各自规则拼接到渠道 API 根地址 |
| 完整地址（`http(s)://` 开头） | `https://host/api/user/self` | **直接使用，不再拼接** |

完整地址写法是必需的逃生口：部分上游的查询接口不在 API 根地址的版本前缀之下
（根地址 `https://host/v1`，余额接口却是 `https://host/api/user/self`），甚至位于另一个
域名，靠拼接无法得到正确地址。

## 三种拼接规则

| 解析函数 | 用于 | 相对路径的拼接方式 |
| --- | --- | --- |
| `resolveEndpointURL` | `models.path`、`costs.path`、`pricing.path`、`endpoints.*.path` | `baseUrl + "/" + path`（补斜杠；不要求前导斜杠） |
| `resolveHostURL` | `quota.path`（相对路径时） | `scheme://host + path`，**丢掉 baseUrl 的路径部分** |
| `resolveQuotaURL` | `quota.path` | 完整地址直连；否则转 `resolveHostURL` |

`quota.path` 之所以特殊：套餐额度接口一般挂在供应商的 host 根下（如智谱
`https://open.bigmodel.cn/api/monitor/usage/quota/limit`），而渠道根地址带着
`/api/coding/paas/v4` 这类版本前缀，直接拼接会拼出错误地址。

## 校验与解析同源

完整地址的判定只有一处实现：`channeltype.IsAbsoluteHTTPURL(value)`
（`url.Parse` 后要求 scheme 为 `http`/`https` 且 host 非空，**scheme 大小写不敏感**）。

配置校验与运行期解析共用它，避免出现「保存成功但查询永远失败」的偏差：

- 校验层 `channeltype.normalizeQuotaConfig` → `validatePathField("quota.path", …)`：
  只接受以 `/` 开头或 `IsAbsoluteHTTPURL` 为真的值。漏掉前导斜杠的写法（`api/user/self`）
  无法判定意图（是 host 根路径还是拼在根地址之后），因此提前拦下。
- 解析层 `channel.resolveQuotaURL` → `channeltype.IsAbsoluteHTTPURL(path)`；
  解析层 `channel.resolveEndpointURL` 等价的判定是 `parsed.IsAbs()` + HTTP(S) scheme 检查。

`models` / `costs` / `pricing` 的路径**不**套用 `validatePathField`：`resolveEndpointURL`
会自动补斜杠，写成 `models` 与 `/models` 等价，加严校验会误伤存量渠道类型。

## 展示层必须与解析规则一致

管理端展示地址时不能无脑 `baseUrl + path`：价格源填了完整地址会被显示成
host 套 host 的怪地址。前端统一走 `frontend/src/lib/priceSource.ts` 的
`priceSourceLocation(source)`，规则与后端一致（完整地址原样返回，相对路径拼接）。

## 存量兼容

生产库中已存在的自定义渠道类型（如 `tokenrhythm`：根地址 `…/v1`、费用路径为完整地址
`…/api/user/self`）本来就是这个用法；`manifest/builtins.json` 里的内置类型全部合规。
因此加严 `quota.path` 校验不会破坏任何存量配置。