import dayjs from 'dayjs'
import timezone from 'dayjs/plugin/timezone'
import utc from 'dayjs/plugin/utc'
import { ref } from 'vue'

dayjs.extend(utc)
dayjs.extend(timezone)

const defaultTimeZone = 'Asia/Shanghai'
export const displayTimeZone = ref(defaultTimeZone)

// 金额展示口径：所有金额在渲染前折算成 displayCurrency。
// 折算只发生在展示层，接口返回的原始金额、结算币种与账单快照都不会被改写。
const supportedCurrencies = ['USD', 'CNY']
export const displayCurrency = ref('USD')
export const exchangeRateBase = ref('USD')
export const exchangeRates = ref<Record<string, number>>({ USD: 1 })
export const exchangeRateSource = ref('')
export const exchangeRateUpdatedAt = ref('')

export const displayCurrencyOptions = [
  { label: '美元（USD）', value: 'USD' },
  { label: '人民币（CNY）', value: 'CNY' },
]

export interface CurrencyConfig {
  display?: string | null
  base?: string | null
  rates?: Record<string, number> | null
  source?: string | null
  updatedAt?: string | null
}

export function setDisplayCurrency(value?: CurrencyConfig | null): void {
  const requested = (value?.display || value?.base || 'USD').toUpperCase()
  displayCurrency.value = supportedCurrencies.includes(requested) ? requested : 'USD'
  exchangeRateBase.value = (value?.base || 'USD').toUpperCase()
  exchangeRates.value = normalizeRates(value?.rates)
  exchangeRateSource.value = value?.source || ''
  exchangeRateUpdatedAt.value = value?.updatedAt || ''
}

// normalizeRates 保证基准币种恒为 1，避免服务端漏发或发来 0 值导致折算结果崩掉。
function normalizeRates(rates?: Record<string, number> | null): Record<string, number> {
  const normalized: Record<string, number> = { USD: 1 }
  for (const [code, rate] of Object.entries(rates || {})) {
    const value = Number(rate)
    if (Number.isFinite(value) && value > 0) normalized[code.toUpperCase()] = value
  }
  return normalized
}

// convertAmount 把金额从原币种折算成展示货币。
// 原币种不在支持范围内时原样返回，宁可不换算也不套错汇率。
export function convertAmount(value: number, currency?: string | null): number {
  const from = (currency || 'USD').toUpperCase()
  const to = displayCurrency.value
  if (from === to) return value
  if (!supportedCurrencies.includes(from) || !supportedCurrencies.includes(to)) return value
  const fromRate = exchangeRates.value[from]
  const toRate = exchangeRates.value[to]
  if (!fromRate || !toRate) return value
  return (value / fromRate) * toRate
}

export function setDisplayTimeZone(value?: string | null): void {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value || defaultTimeZone })
    displayTimeZone.value = value || defaultTimeZone
  } catch {
    displayTimeZone.value = defaultTimeZone
  }
}

export function currentTimeInDisplayZone() {
  return dayjs().tz(displayTimeZone.value)
}

export function formatNumber(value?: number | null): string {
  if (value === undefined || value === null) return '—'
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 }).format(value)
}

export function formatCost(value?: number | string | null, currency = 'USD'): string {
  if (value === undefined || value === null) return '未定价'
	const numericValue = typeof value === 'string' ? Number(value) : value
	if (!Number.isFinite(numericValue)) return '未定价'
  return formatCurrencyAmount(convertAmount(numericValue, currency), displayCurrency.value)
}

// formatBalance 与 formatCost 的区别只在缺失值：余额缺失是「上游没返回」，
// 不是「未定价」，用 — 表示未返回才不会让人误以为价格没配。
export function formatBalance(value?: number | string | null, currency = 'USD'): string {
  if (value === undefined || value === null) return '—'
  return formatCost(value, currency)
}

// formatCurrencyAmount 只负责按指定币种排版，不做折算。
export function formatCurrencyAmount(value: number, currency: string, maximumFractionDigits = 6): string {
  const normalizedCurrency = (currency || 'USD').toUpperCase()
  return new Intl.NumberFormat(normalizedCurrency === 'USD' ? 'en-US' : 'zh-CN', {
    style: 'currency',
    currency: normalizedCurrency,
    minimumFractionDigits: 2,
    maximumFractionDigits,
  }).format(value)
}

export function formatPreciseCost(value?: number | string | null, currency = 'USD'): string {
  if (value === undefined || value === null) return '—'
  const numericValue = typeof value === 'string' ? Number(value) : value
  if (!Number.isFinite(numericValue)) return '—'
  return formatCurrencyAmount(convertAmount(numericValue, currency), displayCurrency.value, 8)
}

export function formatTime(value?: string | null): string {
  if (!value) return '—'
  return dayjs(value).tz(displayTimeZone.value).format('YYYY-MM-DD HH:mm:ss')
}

export function formatReasoningEffort(value?: string | null): string {
  const normalized = value?.trim().toLowerCase()
  if (!normalized) return '默认'
  const labels: Record<string, string> = {
    none: '无', minimal: '最低', low: '低', medium: '中', high: '高', xhigh: '极高', max: '最大', ultra: '超高', auto: '自动',
  }
  return labels[normalized] || value!.trim()
}

export function formatLatency(value?: number | null): string {
  if (value === undefined || value === null) return '—'
  if (value < 1000) return `${value} ms`
  const seconds = value / 1000
  return `${Number.isInteger(seconds) ? seconds : seconds.toFixed(2)} 秒`
}

export function formatUsageDuration(value?: number | null): string {
  if (value === undefined || value === null) return '—'
  const seconds = value / 1000
  if (seconds > 60) {
    const minutes = Math.floor(seconds / 60)
    const remainder = seconds - minutes * 60
    return `${minutes}分${remainder.toFixed(1)}秒`
  }
  return `${seconds.toFixed(1)}秒`
}

export function formatTokenSpeed(outputTokens?: number | null, durationMs?: number | null): string {
	if (outputTokens === undefined || outputTokens === null || durationMs === undefined || durationMs === null) return '—'
	if (durationMs <= 0) return '—'
	return `${Math.round((outputTokens * 1000) / durationMs)}t/s`
}

export function successRate(requests: number, successes: number): string {
  if (!requests) return '—'
  return `${((successes / requests) * 100).toFixed(1)}%`
}
