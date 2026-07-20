import { describe, expect, it } from 'vitest'
import { formatCost, formatLatency, formatNumber, formatTokenSpeed, successRate } from './format'

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

  it('calculates Token speed after the first token arrives or from the total duration', () => {
    expect(formatTokenSpeed(102, 2500, 500)).toBe('51t/s')
    expect(formatTokenSpeed(102, 2500)).toBe('41t/s')
    expect(formatTokenSpeed(102, 500, 500)).toBe('—')
  })
})
