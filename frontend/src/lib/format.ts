import dayjs from 'dayjs'
import timezone from 'dayjs/plugin/timezone'
import utc from 'dayjs/plugin/utc'
import { ref } from 'vue'

dayjs.extend(utc)
dayjs.extend(timezone)

const defaultTimeZone = 'Asia/Shanghai'
export const displayTimeZone = ref(defaultTimeZone)

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
  const normalizedCurrency = (currency || 'USD').toUpperCase()
  return new Intl.NumberFormat(normalizedCurrency === 'USD' ? 'en-US' : 'zh-CN', {
    style: 'currency',
    currency: normalizedCurrency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(numericValue)
}

export function formatPreciseCost(value?: number | string | null, currency = 'USD'): string {
	if (value === undefined || value === null) return '—'
	const numericValue = typeof value === 'string' ? Number(value) : value
	if (!Number.isFinite(numericValue)) return '—'
	const normalizedCurrency = (currency || 'USD').toUpperCase()
	return new Intl.NumberFormat(normalizedCurrency === 'USD' ? 'en-US' : 'zh-CN', {
		style: 'currency',
		currency: normalizedCurrency,
		minimumFractionDigits: 2,
		maximumFractionDigits: 8,
	}).format(numericValue)
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
  if (seconds > 60) return `${(seconds / 60).toFixed(1)} 分钟`
  return `${seconds.toFixed(1)}s`
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
