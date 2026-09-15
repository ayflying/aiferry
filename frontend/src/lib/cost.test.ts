import { afterEach, describe, expect, it } from 'vitest'
import type { CostSummary } from '../api/types'
import { mergeCostSummaries } from './cost'
import { setDisplayCurrency } from './format'

// 折算口径是全局 ref，用例之间必须复位。
afterEach(() => setDisplayCurrency())

const rate = { display: 'USD', base: 'USD', rates: { USD: 1, CNY: 7.2 } }

describe('mergeCostSummaries', () => {
  it('returns undefined when there is nothing to merge', () => {
    expect(mergeCostSummaries(undefined)).toBeUndefined()
    expect(mergeCostSummaries(null)).toBeUndefined()
    expect(mergeCostSummaries([])).toBeUndefined()
  })

  it('converts a single summary into the display currency', () => {
    setDisplayCurrency(rate)
    const summaries: CostSummary[] = [{ currency: 'CNY', usedAmount: 7.2, remainingAmount: 72 }]
    const merged = mergeCostSummaries(summaries)
    // 单条也要折算，否则展示货币切换后这一行会留在原币种。
    expect(merged?.currency).toBe('USD')
    expect(merged?.usedAmount).toBeCloseTo(1, 6)
    expect(merged?.remainingAmount).toBeCloseTo(10, 6)
  })

  it('merges a CNY and a USD summary into one line', () => {
    setDisplayCurrency(rate)
    // 这是基元律动的真实形态：一把人民币账户密钥 + 一把美元账户密钥。
    const summaries: CostSummary[] = [
      { currency: 'CNY', usedAmount: 72, remainingAmount: 720 },
      { currency: 'USD', usedAmount: 3, remainingAmount: 30 },
    ]
    const merged = mergeCostSummaries(summaries)
    expect(merged?.currency).toBe('USD')
    // 72 元 = 10 美元，再与 3 美元相加。
    expect(merged?.usedAmount).toBeCloseTo(13, 6)
    expect(merged?.remainingAmount).toBeCloseTo(130, 6)
  })

  it('sums usage amounts without converting them', () => {
    setDisplayCurrency(rate)
    // 用量的单位是 kToken 之类的计量单位，不是钱，折算没有意义，只能直接相加。
    const summaries: CostSummary[] = [
      { currency: 'CNY', usage: 1200, usageUnit: 'kToken', usageType: '用量', usageDimension: '输入' },
      { currency: 'USD', usage: 800, usageUnit: 'kToken', usageType: '用量', usageDimension: '输入' },
    ]
    const merged = mergeCostSummaries(summaries)
    expect(merged?.usage).toBe(2000)
    expect(merged?.usageUnit).toBe('kToken')
    expect(merged?.usageType).toBe('用量')
    expect(merged?.usageDimension).toBe('输入')
  })

  it('drops fields that no summary provided', () => {
    setDisplayCurrency(rate)
    const merged = mergeCostSummaries([{ currency: 'CNY', usedAmount: 7.2 }, { currency: 'USD', remainingAmount: 5 }])
    expect(merged?.usedAmount).toBeCloseTo(1, 6)
    expect(merged?.remainingAmount).toBeCloseTo(5, 6)
    expect(merged?.usage).toBeUndefined()
    expect(merged?.usageUnit).toBeUndefined()
  })

  it('keeps an explicit zero instead of dropping it', () => {
    setDisplayCurrency(rate)
    // 余额恰好为 0 是有效值，必须保留，不能用真值判断把它当成缺失。
    const merged = mergeCostSummaries([{ currency: 'CNY', usedAmount: 7.2, remainingAmount: 0 }])
    expect(merged?.remainingAmount).toBe(0)
  })
})
