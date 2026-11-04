import type { UsageLog } from '../api/types'
import { formatCost, formatPreciseCost } from './format'

/**
 * 失败请求的本次费用为零；价格快照只用于展示套餐、时段和单价。
 * 两者不能混用：旧版本曾把快照 total 或非零标记价当成失败费用，
 * 即使读到这样的历史数据，也不能再展示为「已计费」。
 */
export function formatFailureCost(log: Pick<UsageLog, 'billingDetails'>): string {
  return formatCost(0, log.billingDetails?.currency)
}

export function formatFailureCostPrecise(log: Pick<UsageLog, 'billingDetails'>): string {
  return formatPreciseCost(0, log.billingDetails?.currency)
}
