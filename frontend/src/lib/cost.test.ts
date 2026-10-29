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

  it('keeps credit (XXX) totals out of money conversion', () => {
    setDisplayCurrency(rate)
    // WorkBuddy 积分是计数口径，币种标记为 XXX（无货币）。1500 积分绝不能被
    // 当成 1500 美元再折算成 10800 人民币，也不能并入美元金额里。
    const merged = mergeCostSummaries([{ currency: 'XXX', usedAmount: 1500, remainingAmount: 1500 }])
    expect(merged?.currency).toBe('XXX')
    expect(merged?.usedAmount).toBe(1500)
    expect(merged?.remainingAmount).toBe(1500)
  })

  it('sums credit used and remaining separately', () => {
    setDisplayCurrency(rate)
    // 两把密钥各自有已用与余额，合并后必须分别求和，不能把已用当余额覆盖。
    const merged = mergeCostSummaries([
      { currency: 'XXX', usedAmount: 100, remainingAmount: 900 },
      { currency: 'XXX', usedAmount: 40, remainingAmount: 0 },
    ])
    expect(merged?.currency).toBe('XXX')
    expect(merged?.usedAmount).toBe(140)
    expect(merged?.remainingAmount).toBe(900)
  })

  it('ignores money rows for credits channels when countersOnly is set', () => {
    setDisplayCurrency(rate)
    // 纯积分渠道会同时带网关按定价累计的美元费用与积分余额：单位不同不能混。
    // 2026-10-27 生产上因此出现「USD 积分余额 剩余 0.04 积分」的错误汇总。
    const summaries = [
      { currency: 'XXX', usedAmount: 0, remainingAmount: 0 },
      { currency: 'USD', usedAmount: 0.038418 },
    ]
    const merged = mergeCostSummaries(summaries, { countersOnly: true })
    expect(merged?.currency).toBe('XXX')
    expect(merged?.usedAmount).toBe(0)
    expect(merged?.remainingAmount).toBe(0)
  })

  it('returns an empty credit summary when no counter row exists', () => {
    setDisplayCurrency(rate)
    // 查询全失败时只剩美元行：计数口径下不应把美元金额当成积分展示。
    const merged = mergeCostSummaries([{ currency: 'USD', usedAmount: 0.038418 }], { countersOnly: true })
    expect(merged?.currency).toBe('XXX')
    expect(merged?.usedAmount).toBeUndefined()
    expect(merged?.remainingAmount).toBeUndefined()
  })

  it('keeps credit totals when the counter rows also carry usage metadata', () => {
    setDisplayCurrency(rate)
    // 后端曾把「已用积分」镜像成 usage（并把 usageType/UsageUnit 补成积分），
    // 前端「剩余」又优先取 usage，于是页头把已用合计当余额显示
    // （2026-10-29 生产截图：337.93+152.21+52.34=542.48）。
    // 修好后 summarizes 只有已用/余额，计数口径仍必须落在 XXX 上而不是被
    // 金额行的 USD 带偏。
    const summaries = [
      { currency: 'XXX', usedAmount: 542.48, remainingAmount: 7297.52, usageType: '积分余额', usageUnit: '积分' },
      { currency: 'USD', usedAmount: 0.038418 },
    ]
    const merged = mergeCostSummaries(summaries, { countersOnly: true })
    expect(merged?.currency).toBe('XXX')
    expect(merged?.usedAmount).toBe(542.48)
    expect(merged?.remainingAmount).toBe(7297.52)
    // 没有 usage 时不能凭空造一个，否则「剩余」又会拿到已用。
    expect(merged?.usage).toBeUndefined()
  })
})
