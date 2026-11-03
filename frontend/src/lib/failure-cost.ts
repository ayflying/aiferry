import type { BillingDetails, UsageLog } from '../api/types'
import { formatCost, formatPreciseCost } from './format'
import { isSuccessfulUsageLog } from './usage-columns'

/**
 * 失败请求的计费展示口径。
 *
 * 背景：网关对失败的调用同样消耗了上游 token，后端会把那次的计费快照落库，
 * 并把 estimated_cost 写成标记价（非 0、但远小于结算精度）。
 * 前端要做的只是把真实金额取回来展示，而不是把标记价当成金额渲染。
 *
 * 三类记录必须区分清楚，否则用户会看到「失败 + 未定价」这种自相矛盾的组合：
 * 1. 有计费快照的失败记录：显示快照里的真实金额；
 * 2. 没有快照、成本为 0 的失败记录：显示 $0.00（明确「没有产生费用」）；
 * 3. 既没有快照、也没有成本字段的远古记录：才允许显示「未定价」。
 */

/** 与后端 usage.failureCostSentinel 对应：标记价不超过这个数量级时一律按标记处理。 */
const FAILURE_COST_SENTINEL_CEILING = 1e-6

/** 从「请求 + 计费快照」得出的展示金额（元信息，不参与换算）。 */
export interface FailureCostView {
  /** 是否有可直接展示的金额。 */
  amount?: number
  /** 金额所在的币种，用于跟随展示货币折算。 */
  currency?: string
  /** 金额是否来自计费快照（而不是落库的标记价/真实成本）。 */
  fromSnapshot: boolean
}

export function isSuccessfulLog(log: Pick<UsageLog, 'httpStatus'>): boolean {
  return isSuccessfulUsageLog(log)
}

/** isFailureButBilled 判断这条失败记录的金额是否来自计费快照（用于文案区分「已计费」）。 */
export function isFailureButBilled(log: Pick<UsageLog, 'httpStatus' | 'estimatedCost' | 'billingDetails'>): boolean {
  return !isSuccessfulUsageLog(log) && failureCostView(log).fromSnapshot
}

/**
 * failureCostView 解析失败记录要展示的金额。
 *
 * 标记价本身也在 estimatedCost 里，但它不是金额，直接 formatCost 会渲染出
 * `$0.00`，看起来像「这笔失败免费」，所以优先用快照里的真实 total。
 */
export function failureCostView(log: Pick<UsageLog, 'estimatedCost' | 'billingDetails'>): FailureCostView {
  const snapshotTotal = snapshotTotalOf(log.billingDetails)
  if (snapshotTotal !== undefined) {
    return { amount: snapshotTotal, currency: log.billingDetails?.currency, fromSnapshot: true }
  }
  const recorded = log.estimatedCost
  if (recorded === undefined || recorded === null) return { fromSnapshot: false }
  // 标记价没有快照可依时不能冒充金额：它只是「当时算过费」的记号。
  if (Math.abs(recorded) < FAILURE_COST_SENTINEL_CEILING) return { fromSnapshot: false }
  return { amount: recorded, fromSnapshot: false }
}

/** isFailureSentinel 判断一个成本值是不是「失败已计费」的标记价。 */
export function isFailureSentinel(value?: number | null): boolean {
  return value !== undefined && value !== null && Math.abs(value) < FAILURE_COST_SENTINEL_CEILING
}

function snapshotTotalOf(details?: BillingDetails): number | undefined {
  if (!details) return undefined
  const total = Number(details.total)
  return Number.isFinite(total) ? total : undefined
}

/**
 * formatFailureCost 按失败记录的实际金额排版：先还原真实金额，再交给 formatCost 折算。
 * 所有显示失败成本的页面都必须走这里，避免各自复制一套「先取快照再格式化」的逻辑。
 * 没有可展示金额时按 0 展示——失败请求确实可能没有成本，但那不等于「未定价」。
 */
export function formatFailureCost(log: Pick<UsageLog, 'estimatedCost' | 'billingDetails'>): string {
  const view = failureCostView(log)
  return formatCost(view.amount ?? 0, view.currency)
}

/** formatFailureCostPrecise 给详情弹窗用：保留到分位以下，避免小额成本被四舍五入成 $0.00。 */
export function formatFailureCostPrecise(log: Pick<UsageLog, 'estimatedCost' | 'billingDetails'>): string {
  const view = failureCostView(log)
  return formatPreciseCost(view.amount ?? 0, view.currency)
}
