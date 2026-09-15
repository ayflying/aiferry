import type { CostSummary } from '../api/types'
import { convertAmount, displayCurrency } from './format'

// mergeCostSummaries 把一条渠道按币种分组的费用汇总折算到展示货币后合并成一条。
//
// 为什么要合并：同一渠道的多把上游密钥可能挂在不同币种的账户上
// （例如基元律动一把人民币账户、一把美元账户，且人民币账户来自上游余额查询、
// 美元账户来自网关按定价累计的费用），接口会按币种返回多行。
// 列表里并列多行既占位置、又没法比较，折算到同一货币后求和才是同一口径。
//
// 用法模式：费用/余额模式下折算 usedAmount 与 remainingAmount；
// 用量模式下折算没有意义（单位是 kToken 等），但求和依然成立，直接相加。
export function mergeCostSummaries(summaries?: CostSummary[] | null): CostSummary | undefined {
  if (!summaries || summaries.length === 0) return undefined
  const merged: CostSummary = { currency: displayCurrency.value }
  let usedTotal = 0
  let remainingTotal = 0
  let hasUsed = false
  let hasRemaining = false
  let usageTotal = 0
  let hasUsage = false
  for (const summary of summaries) {
    if (summary.usedAmount !== undefined && summary.usedAmount !== null) {
      usedTotal += convertAmount(summary.usedAmount, summary.currency)
      hasUsed = true
    }
    if (summary.remainingAmount !== undefined && summary.remainingAmount !== null) {
      remainingTotal += convertAmount(summary.remainingAmount, summary.currency)
      hasRemaining = true
    }
    if (summary.usage !== undefined && summary.usage !== null) {
      usageTotal += summary.usage
      hasUsage = true
    }
    // 用量单位与维度由渠道类型声明，各币种行一致，取第一条即可。
    if (!merged.usageUnit && summary.usageUnit) merged.usageUnit = summary.usageUnit
    if (!merged.usageType && summary.usageType) merged.usageType = summary.usageType
    if (!merged.usageDimension && summary.usageDimension) merged.usageDimension = summary.usageDimension
  }
  if (hasUsed) merged.usedAmount = usedTotal
  if (hasRemaining) merged.remainingAmount = remainingTotal
  if (hasUsage) merged.usage = usageTotal
  return merged
}
