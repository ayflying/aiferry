import type { ModelBillingMode, PublicModel, TimeWindow } from '../api/types'
import { describeTimeWindow, readTimeWindow } from './time-window'

type TokenPriceField = Pick<PublicModel,
  | 'inputPrice'
  | 'cachedInputPrice'
  | 'cacheWritePrice'
  | 'outputPrice'
  | 'imageInputPrice'
  | 'audioInputPrice'
  | 'audioOutputPrice'>

export interface TokenPriceItem {
  label: string
  value: number
}

const tokenPriceFields: Array<{ label: string; field: keyof TokenPriceField }> = [
  { label: '输入', field: 'inputPrice' },
  { label: '缓存读', field: 'cachedInputPrice' },
  { label: '缓存写', field: 'cacheWritePrice' },
  { label: '输出', field: 'outputPrice' },
  { label: '图像输入', field: 'imageInputPrice' },
  { label: '音频输入', field: 'audioInputPrice' },
  { label: '音频输出', field: 'audioOutputPrice' },
]

export function configuredTokenPriceItems(model: TokenPriceField): TokenPriceItem[] {
  return tokenPriceFields.flatMap(({ label, field }) => {
    const value = model[field]
    return typeof value === 'number' && Number.isFinite(value) ? [{ label, value }] : []
  })
}

export function formatModelPrice(value: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 }).format(value)
}

export function modelBillingModeLabel(mode: ModelBillingMode): string {
  switch (mode) {
    case 'request': return '按请求'
    case 'rules': return '高级规则'
    default: return '按 Token'
  }
}

export type PriceRuleDraft = {
  name: string
  priority: number
  currency: string
  conditions: Record<string, unknown>
  rates: Record<string, number>
}

export function createPriceRuleDraft(): PriceRuleDraft {
  return { name: '', priority: 100, currency: 'USD', conditions: {}, rates: {} }
}

// 计费规则的时段条件与「模型关闭时间」共用同一套 TimeWindow 结构，
// 这里只负责从 conditions 里取出 time 块，摘要与判定复用公共实现。
function readTimeBlock(conditions?: Record<string, unknown> | null): TimeWindow | null {
  return readTimeWindow(conditions?.['time'])
}

// 判断规则的生效时段是否真的收窄了范围：只有「部分星期」或「显式时段区间」才算受限，
// 全选七天、只写时区或没有 time 块都等价于全天生效。
export function priceRuleTimeIsRestricted(conditions?: Record<string, unknown> | null): boolean {
  return readTimeBlock(conditions) !== null
}

export function describePriceRuleTime(conditions?: Record<string, unknown> | null): string {
  const time = readTimeBlock(conditions)
  if (!time) return '不限时段'
  return describeTimeWindow(time)
}

export function describePriceRuleConditions(conditions?: Record<string, unknown> | null): string {
  const endpoint = typeof conditions?.['endpoint'] === 'string' ? conditions['endpoint'].trim() : ''
  const time = describePriceRuleTime(conditions)
  return endpoint ? `${endpoint} · ${time}` : time
}
