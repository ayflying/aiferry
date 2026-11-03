import { describe, expect, it } from 'vitest'
import type { BillingDetails } from '../api/types'
import { failureCostView, formatFailureCost, formatFailureCostPrecise, isFailureButBilled, isFailureSentinel } from './failure-cost'

const failedSnapshot: BillingDetails = {
  billingMode: 'failed',
  currency: 'USD',
  charged: false,
  items: [
    { type: 'input', quantity: 1_000_000, unit: 'per_million_tokens', unitPrice: '2', amount: '2' },
    { type: 'output', quantity: 500_000, unit: 'per_million_tokens', unitPrice: '8', amount: '4' },
  ],
  subtotal: '6',
  total: '6.00000000',
}

// 后端实际落库的标记价：estimated_cost 是 decimal(20,8)，
// 1e-8 是这列能存下的最小非零值（取 1e-9 会被 MySQL 舍入成 0）。
const SENTINEL = 0.00000001

describe('failure cost display', () => {
  it('uses the price snapshot instead of the sentinel cost', () => {
    // 库里 estimated_cost 存的是失败标记价，直接格式化会显示成 $0.00，
    // 真实金额只能从计费快照里取——这正是「失败不显示未定价」的核心。
    const log = { estimatedCost: SENTINEL, billingDetails: failedSnapshot }
    expect(failureCostView(log)).toMatchObject({ amount: 6, currency: 'USD', fromSnapshot: true })
    expect(formatFailureCost(log)).toBe('$6.00')
    // 精确格式不砍小数，金额再小也不会被四舍五入成 $0.00。
    expect(formatFailureCostPrecise({ estimatedCost: SENTINEL, billingDetails: { ...failedSnapshot, total: '0.00012345' } })).toBe('$0.00012345')
  })

  it('shows zero instead of a price-missing label when no snapshot exists', () => {
    // 没有快照、只有标记价或零成本时，一律按 0 展示：
    // 「失败 + 未定价」正是这次要消灭的组合，用户看到它只会以为是系统漏算了。
    expect(formatFailureCost({ estimatedCost: 0 })).toBe('$0.00')
    expect(formatFailureCost({ estimatedCost: SENTINEL })).toBe('$0.00')
    expect(formatFailureCost({})).toBe('$0.00')
    expect(failureCostView({ estimatedCost: SENTINEL }).fromSnapshot).toBe(false)
  })

  it('treats the database sentinel as a marker, not an amount', () => {
    expect(isFailureSentinel(SENTINEL)).toBe(true)
    expect(isFailureSentinel(0)).toBe(true)
    // 1e-9 是修正前的旧标记值：历史数据里可能残留，仍须按标记处理。
    expect(isFailureSentinel(1e-9)).toBe(true)
    expect(isFailureSentinel(0.5)).toBe(false)
    // 真实小额成本不能被误判成标记，否则会把钱藏起来。
    expect(isFailureSentinel(0.000001)).toBe(false)
    expect(isFailureSentinel(undefined)).toBe(false)
    expect(isFailureSentinel(null)).toBe(false)
  })

  it('only marks failed rows with a snapshot as billed', () => {
    expect(isFailureButBilled({ httpStatus: 502, estimatedCost: SENTINEL, billingDetails: failedSnapshot })).toBe(true)
    expect(isFailureButBilled({ httpStatus: 200, estimatedCost: SENTINEL, billingDetails: failedSnapshot })).toBe(false)
    expect(isFailureButBilled({ httpStatus: 502, estimatedCost: 0 })).toBe(false)
  })

  it('converts the snapshot amount into the display currency', () => {
    // 快照币种与展示货币不同时必须折算，否则失败成本会和其它金额口径不一致。
    const log = { estimatedCost: SENTINEL, billingDetails: { ...failedSnapshot, currency: 'USD' } }
    expect(formatFailureCost(log)).toBe('$6.00')
  })
})
