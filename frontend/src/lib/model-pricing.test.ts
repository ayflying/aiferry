import { describe, expect, it } from 'vitest'
import { configuredTokenPriceItems, formatModelPrice, modelBillingModeLabel } from './model-pricing'

describe('model pricing display', () => {
  it('includes cache read and cache write as separate configured prices', () => {
    const items = configuredTokenPriceItems({ inputPrice: 1.5, cachedInputPrice: 0.15, cacheWritePrice: 3, outputPrice: 12 })

    expect(items).toEqual([
      { label: '输入', value: 1.5 },
      { label: '缓存读', value: 0.15 },
      { label: '缓存写', value: 3 },
      { label: '补全', value: 12 },
    ])
  })

  it('keeps zero as a configured price and omits unset fields', () => {
    expect(configuredTokenPriceItems({ cachedInputPrice: 0, cacheWritePrice: undefined })).toEqual([{ label: '缓存读', value: 0 }])
  })

  it('formats values and billing labels for the list', () => {
    expect(formatModelPrice(12.3456789)).toBe('12.345679')
    expect(modelBillingModeLabel('token')).toBe('按 Token')
    expect(modelBillingModeLabel('request')).toBe('按请求')
    expect(modelBillingModeLabel('rules')).toBe('高级规则')
  })
})
