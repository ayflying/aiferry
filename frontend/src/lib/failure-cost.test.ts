import { describe, expect, it } from 'vitest'
import type { BillingDetails } from '../api/types'
import { formatFailureCost, formatFailureCostPrecise } from './failure-cost'

const snapshot: BillingDetails = {
  billingMode: 'token', currency: 'USD', charged: false,
  items: [{ type: 'input', quantity: 0, unit: 'per_million_tokens', unitPrice: '2', amount: '0' }],
  subtotal: '0', total: '0',
}

describe('failure cost display', () => {
  it('有套餐费率也只显示零费用', () => {
    expect(formatFailureCost({ billingDetails: snapshot })).toBe('$0.00')
    expect(formatFailureCostPrecise({ billingDetails: snapshot })).toBe('$0.00')
    expect(snapshot.items[0]?.unitPrice).toBe('2')
  })
  it('历史错误快照的非零金额不能冒充失败费用', () => {
    const old = { ...snapshot, subtotal: '6', total: '6.00000000' }
    expect(formatFailureCost({ billingDetails: old })).toBe('$0.00')
    expect(formatFailureCostPrecise({ billingDetails: old })).toBe('$0.00')
  })
  it('无快照仍显示零而不是未定价', () => {
    expect(formatFailureCost({})).toBe('$0.00')
  })
})
