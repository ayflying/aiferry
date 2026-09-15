import { afterEach, describe, expect, it } from 'vitest'
import { convertAmount, displayCurrency, formatBalance, formatCost, formatLatency, formatNumber, formatReasoningEffort, formatTime, formatTokenSpeed, formatUsageDuration, setDisplayCurrency, setDisplayTimeZone, successRate } from './format'

describe('format helpers', () => {
  it('does not report missing prices as zero', () => {
    expect(formatCost(undefined)).toBe('未定价')
  })

  it('uses the dollar sign for USD', () => {
    expect(formatCost(2.5)).toBe('$2.50')
  })

  it('formats token counts', () => {
    expect(formatNumber(12345)).toBe('12,345')
  })

  it('calculates success rate', () => {
    expect(successRate(8, 6)).toBe('75.0%')
  })

  it('uses seconds for test latency at or above one second', () => {
    expect(formatLatency(328)).toBe('328 ms')
    expect(formatLatency(1000)).toBe('1 秒')
    expect(formatLatency(2544)).toBe('2.54 秒')
  })

	it('calculates Token speed from the complete request duration', () => {
		expect(formatTokenSpeed(102, 2500)).toBe('41t/s')
		expect(formatTokenSpeed(511, 52900)).toBe('10t/s')
		expect(formatTokenSpeed(102, 0)).toBe('—')
	})

  it('formats reasoning effort for compact usage rows', () => {
    expect(formatReasoningEffort()).toBe('默认')
    expect(formatReasoningEffort('high')).toBe('高')
    expect(formatReasoningEffort('xhigh')).toBe('极高')
    expect(formatReasoningEffort('custom')).toBe('custom')
  })

  it('formats UTC timestamps in the configured display timezone', () => {
    setDisplayTimeZone('UTC')
    expect(formatTime('2026-07-21T02:22:47Z')).toBe('2026-07-21 02:22:47')
    setDisplayTimeZone('Asia/Shanghai')
    expect(formatTime('2026-07-21T02:22:47Z')).toBe('2026-07-21 10:22:47')
  })

  it('formats usage durations with Chinese units', () => {
    expect(formatUsageDuration()).toBe('—')
    expect(formatUsageDuration(60_000)).toBe('60.0秒')
    expect(formatUsageDuration(60_001)).toBe('1分0.0秒')
    expect(formatUsageDuration(90_000)).toBe('1分30.0秒')
    expect(formatUsageDuration(84_000)).toBe('1分24.0秒')
    expect(formatUsageDuration(210_000)).toBe('3分30.0秒')
  })
})

// displayCurrency 是全局 ref，用例之间必须复位，否则后面的用例会继承上一个用例的折算口径。
afterEach(() => setDisplayCurrency())

describe('currency conversion', () => {
  const cnyRate = { display: 'USD', base: 'USD', rates: { USD: 1, CNY: 7.2 } }

  it('converts a CNY amount into the USD display currency', () => {
    setDisplayCurrency(cnyRate)
    // 7.2 元按「1 USD = 7.2 CNY」折算应当是 1 美元。
    expect(formatCost(7.2, 'CNY')).toBe('$1.00')
    expect(convertAmount(7.2, 'CNY')).toBeCloseTo(1, 6)
  })

  it('keeps amounts already in the display currency unchanged', () => {
    setDisplayCurrency({ ...cnyRate, display: 'CNY' })
    expect(formatCost(7.2, 'CNY')).toBe('¥7.20')
    expect(convertAmount(7.2, 'CNY')).toBeCloseTo(7.2, 6)
  })

  it('does not touch currencies it has no rate for', () => {
    setDisplayCurrency(cnyRate)
    // 没有汇率就不猜，原样返回才不会把金额算错。
    expect(convertAmount(100, 'JPY')).toBe(100)
  })

  it('treats a missing currency as USD', () => {
    setDisplayCurrency({ ...cnyRate, display: 'CNY' })
    expect(convertAmount(1, undefined)).toBeCloseTo(7.2, 6)
    expect(convertAmount(1, null)).toBeCloseTo(7.2, 6)
  })
})

describe('display currency settings', () => {
  it('falls back to USD for unsupported or missing values', () => {
    setDisplayCurrency({ display: 'JPY', base: 'USD', rates: { USD: 1 } })
    expect(displayCurrency.value).toBe('USD')
    setDisplayCurrency()
    expect(displayCurrency.value).toBe('USD')
  })

  it('ignores non-positive rates so conversion never divides by zero', () => {
    setDisplayCurrency({ display: 'CNY', base: 'USD', rates: { USD: 1, CNY: 0 } })
    expect(convertAmount(7.2, 'USD')).toBe(7.2)
  })
})

describe('formatBalance', () => {
  it('shows a dash when the upstream returned no balance', () => {
    // 余额缺失是「上游没返回」，不能写成「未定价」，否则会被误读成没配价格。
    expect(formatBalance(undefined)).toBe('—')
    expect(formatBalance(null)).toBe('—')
  })

  it('formats a present balance like a cost', () => {
    expect(formatBalance(1.5)).toBe('$1.50')
  })
})
