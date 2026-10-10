# 模型名字：映射名与上游原始名双入口路由

从 `v2.10.0` 开始，渠道「模型映射」里配置的上游模型名和映射后的公开名称都可以直接作为请求体 `model` 字段的值来路由。例如：

- `open-codeZen` 渠道里 `mimo-v2.6-flash-free` → `free`
- 同渠道 `space-bunny-free` → `free`

请求 `free`、`mimo-v2.6-flash-free`、`space-bunny-free` 三个字符串都可以成功路由到同一批候选，计费和上游请求仍按候选的公开名（`free`）执行。

## 相关代码位置

- `internal/logic/relay/route_cache.go`：`routeStatic` 同时按 `public_name` 与 `upstream_name` 匹配。
- `internal/logic/relay/routing.go`：`routeWithPolicy` 把密钥白名单判定放在路由之后，以候选公开名为准。
- `internal/logic/relay/usage.go`：`candidatesRequireBalanceCheck` 按候选公开名决定余额预检，不受请求字符串影响。
