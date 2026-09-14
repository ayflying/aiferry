import type { ModelBillingMode, PublicModel } from '../api/types'

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

const weekdayLabels = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

function readTimeBlock(conditions?: Record<string, unknown> | null): Record<string, unknown> | null {
  const block = conditions?.['time']
  if (!block || typeof block !== 'object' || Array.isArray(block)) return null
  return block as Record<string, unknown>
}

function readWeekdays(time: Record<string, unknown>): number[] {
  if (!Array.isArray(time.weekdays)) return []
  return [...new Set(time.weekdays.filter((item): item is number => typeof item === 'number' && item >= 1 && item <= 7))].sort((left, right) => left - right)
}

function readClockPairs(time: Record<string, unknown>): Array<[string, string]> {
  if (!Array.isArray(time.ranges)) return []
  return time.ranges
    .filter((item): item is unknown[] => Array.isArray(item) && item.length === 2)
    .map((item) => [String(item[0]), String(item[1])] as [string, string])
}

function formatWeekdays(days: number[]): string {
  if (!days.length || days.length === 7) return ''
  const segments: string[] = []
  let start = days[0]
  let previous = days[0]
  for (let index = 1; index <= days.length; index += 1) {
    const current = days[index]
    if (current === previous + 1) {
      previous = current
      continue
    }
    segments.push(start === previous ? weekdayLabels[start - 1] : `${weekdayLabels[start - 1]}至${weekdayLabels[previous - 1]}`)
    if (current === undefined) break
    start = current
    previous = current
  }
  return segments.join('、')
}

// 判断规则的生效时段是否真的收窄了范围：只有「部分星期」或「显式时段区间」才算受限，
// 全选七天或没有 time 块都等价于全天生效。
export function priceRuleTimeIsRestricted(conditions?: Record<string, unknown> | null): boolean {
  const time = readTimeBlock(conditions)
  if (!time) return false
  return formatWeekdays(readWeekdays(time)) !== '' || readClockPairs(time).length > 0
}

export function describePriceRuleTime(conditions?: Record<string, unknown> | null): string {
  const time = readTimeBlock(conditions)
  if (!time) return '不限时段'
  const weekdays = formatWeekdays(readWeekdays(time))
  const ranges = readClockPairs(time)
    .map(([start, end]) => `${start}–${end}`)
    .join('、')
  if (!ranges) return weekdays || '不限时段'
  return `${weekdays || '每天'} ${ranges}`
}

export function describePriceRuleConditions(conditions?: Record<string, unknown> | null): string {
  const endpoint = typeof conditions?.['endpoint'] === 'string' ? conditions['endpoint'].trim() : ''
  const time = describePriceRuleTime(conditions)
  return endpoint ? `${endpoint} · ${time}` : time
}
