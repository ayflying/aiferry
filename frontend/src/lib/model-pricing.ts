import type { ModelBillingMode, PriceRule, PublicModel, TimeWindow } from '../api/types'
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

// 编辑已有规则时把规则回填成编辑器草稿。
// conditions/rates 必须拷贝：抽屉里的规则列表和编辑器共用同一份数据，
// 直接引用会让编辑器内的临时改动立刻污染列表展示。
export function priceRuleToDraft(rule: PriceRule): PriceRuleDraft {
  return {
    name: rule.name ?? '',
    priority: Number.isFinite(rule.priority) ? rule.priority : 100,
    currency: rule.currency?.trim() || 'USD',
    conditions: { ...(rule.conditions ?? {}) },
    rates: { ...(rule.rates ?? {}) },
  }
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

// 上游同步的上下文阶梯与峰谷时段相乘，列表须分别说明两种条件，
// 不可把无 time 条件的兜底分支误写成“全天同价”。
export function describePriceRuleTier(rule: PriceRule, allRules: PriceRule[]): string {
  const conditions = rule.conditions ?? {}
  const minimum = conditions['inputTokensAtLeast']
  const maximum = conditions['inputTokensAtMost']
  const hasMin = typeof minimum === 'number' && Number.isFinite(minimum)
  const hasMax = typeof maximum === 'number' && Number.isFinite(maximum)
  const length = hasMin && hasMax
    ? `输入 ${formatModelPrice(minimum)}–${formatModelPrice(maximum)} Token`
    : hasMin ? `输入 ≥ ${formatModelPrice(minimum)} Token`
      : hasMax ? `输入 ≤ ${formatModelPrice(maximum)} Token` : '不限输入长度'
  const time = describePriceRuleTime(conditions)
  const hasPeakSibling = !priceRuleTimeIsRestricted(conditions) && allRules.some((other) =>
    other.id !== rule.id && other.status === 1 && priceRuleTimeIsRestricted(other.conditions)
    && other.conditions?.['inputTokensAtLeast'] === minimum
    && other.conditions?.['inputTokensAtMost'] === maximum,
  )
  return `${length} · ${hasPeakSibling ? '其他时段（高峰规则优先）' : time}`
}

const priceRuleRateFields = [
  ['inputPerMillion', '输入'], ['outputPerMillion', '输出'],
  ['cachedInputPerMillion', '缓存读取'], ['cacheWritePerMillion', '缓存写入'],
  ['imageInputPerMillion', '图像输入'], ['audioInputPerMillion', '音频输入'],
  ['audioOutputPerMillion', '音频输出'], ['request', '每请求'],
] as const

export function describePriceRuleRates(rule: PriceRule): string[] {
  return priceRuleRateFields.flatMap(([key, label]) => {
    const value = rule.rates?.[key]
    if (typeof value !== 'number' || !Number.isFinite(value)) return []
    return [`${label} ${rule.currency === 'CNY' ? '¥' : rule.currency === 'USD' ? '$' : rule.currency + ' '}${formatModelPrice(value)}${key === 'request' ? ' / 请求' : ' / 1M Token'}`]
  })
}

export function describePriceRuleConditions(conditions?: Record<string, unknown> | null): string {
  const endpoint = typeof conditions?.['endpoint'] === 'string' ? conditions['endpoint'].trim() : ''
  const time = describePriceRuleTime(conditions)
  return endpoint ? `${endpoint} · ${time}` : time
}
