import { describe, expect, it } from 'vitest'
import { formatCompactDuration, formatCompactTokens } from './compact-format'

describe('仪表盘紧凑数值', () => {
  it.each([
    [0, '0'], [999, '999'], [1000, '1K'], [12345, '12.35K'],
    [1_234_567, '1.23M'], [1_234_567_890, '1.23B'], [1e12, '1T'],
    [999_999, '1M'], [-12345, '-12.35K'],
  ])('Token %s 显示为 %s', (value, expected) => {
    expect(formatCompactTokens(value)).toBe(expected)
  })
  it.each([
    [0, '0 ms'], [328, '328 ms'], [999, '999 ms'], [1000, '1 秒'],
    [2544, '2.54 秒'], [55_419, '55.42 秒'], [60_000, '1 分钟'],
    [90_000, '1.5 分钟'], [3_600_000, '1 小时'], [86_400_000, '1 天'],
    [59_999, '1 分钟'], [999.999, '1 秒'],
  ])('耗时 %s 显示为 %s', (value, expected) => {
    expect(formatCompactDuration(value)).toBe(expected)
  })
  it.each([undefined, null, NaN, Infinity, -Infinity])('缺失或非法值 %s 不显示错误数字', value => {
    expect(formatCompactTokens(value)).toBe('—')
    expect(formatCompactDuration(value)).toBe('—')
  })
})
