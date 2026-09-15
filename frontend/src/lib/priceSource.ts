import type { PriceSource } from '../api/types'

// 价格源的「地址」展示的是运行期实际请求的地址。渠道类型/价格源配置里的路径字段
// 支持两种写法：以 / 开头的相对路径（拼接到根地址之后），或 http(s):// 开头的
// 完整地址（直接使用、不再拼接——有些上游的价格接口不在根地址之下，甚至位于
// 另一个域名）。展示层必须与后端解析规则一致，否则填了完整地址的源会显示成
// host 套 host 的怪地址。
export function priceSourceLocation(source: PriceSource): string {
  const baseURL = String(source.config?.baseUrl ?? '')
  const path = String(source.config?.pricing?.path ?? '')
  if (/^https?:\/\//i.test(path)) return path
  return `${baseURL}${path}`
}
