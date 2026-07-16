import { describe, expect, it } from 'vitest'
import { formatCost, formatNumber, successRate } from './format'

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
})
