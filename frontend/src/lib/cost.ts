import type { CostSummary } from '../api/types'
import { convertAmount, displayCurrency, usageCurrency } from './format'

// mergeCostSummaries 把一条渠道按币种分组的费用汇总折算到展示货币后合并成一条。
//
// 为什么要合并：同一渠道的多把上游密钥可能挂在不同币种的账户上
// （例如基元律动一把人民币账户、一把美元账户，且人民币账户来自上游余额查询、
// 美元账户来自网关按定价累计的费用），接口会按币种返回多行。
// 列表里并列多行既占位置、又没法比较，折算到同一货币后求和才是同一口径。
//
// 用法模式：费用/余额模式下折算 usedAmount 与 remainingAmount；
// 用量模式下折算没有意义（单位是 kToken、积分等），但求和依然成立，直接相加。
//
// 计数口径（XXX＝无货币）单独处理：它绝不能参与货币折算与合并到展示货币的
// 金额里，否则「积分 1500 抵 1500 美元」这种错误金额会出现在渠道汇总上。
export function mergeCostSummaries(summaries?: CostSummary[] | null): CostSummary | undefined {
  if (!summaries || summaries.length === 0) return undefined
  const merged: CostSummary = { currency: displayCurrency.value }
  let usedTotal = 0
  let remainingTotal = 0
  let hasUsed = false
  let hasRemaining = false
  let usageTotal = 0
  let hasUsage = false
  // 计数口径的合计单独记账，代表值取第一条的币种标记（XXX）。
  let countTotal = 0
  let hasCount = false
  for (const summary of summaries) {
    const counter = (summary.currency || '').toUpperCase() === usageCurrency
    if (summary.usedAmount !== undefined && summary.usedAmount !== null) {
      if (counter) {
        countTotal += summary.usedAmount
        hasCount = true
      } else {
        usedTotal += convertAmount(summary.usedAmount, summary.currency)
        hasUsed = true
      }
    }
    if (summary.remainingAmount !== undefined && summary.remainingAmount !== null && !counter) {
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
  if (hasCount && !hasUsed && !hasRemaining && !hasUsage) {
    // 纯计数口径（如 WorkBuddy 积分）：保留「无货币」标记并给出已用量（余额）。
    merged.currency = usageCurrency
    merged.usedAmount = countTotal
    merged.remainingAmount = countTotal
    return merged
  }
  if (hasUsed) merged.usedAmount = usedTotal
  if (hasRemaining) merged.remainingAmount = remainingTotal
  if (hasUsage) merged.usage = usageTotal
  return merged
}
