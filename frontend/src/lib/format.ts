import dayjs from 'dayjs'

export function formatNumber(value?: number | null): string {
  if (value === undefined || value === null) return '—'
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 }).format(value)
}

export function formatCost(value?: number | null, currency = 'USD'): string {
  if (value === undefined || value === null) return '未定价'
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: currency || 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(value)
}

export function formatTime(value?: string | null): string {
  if (!value) return '—'
  return dayjs(value).format('YYYY-MM-DD HH:mm:ss')
}

export function successRate(requests: number, successes: number): string {
  if (!requests) return '—'
  return `${((successes / requests) * 100).toFixed(1)}%`
}
