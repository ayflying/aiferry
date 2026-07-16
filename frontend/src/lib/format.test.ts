import { describe, expect, it } from 'vitest'
import { formatCost, formatLatency, formatNumber, successRate } from './format'

describe('format helpers', () => {
  it('does not report missing prices as zero', () => {
    expect(formatCost(undefined)).toBe('未定价')
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
})
