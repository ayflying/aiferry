import { describe, expect, it } from 'vitest'
import { configuredTokenPriceItems, createPriceRuleDraft, describePriceRuleConditions, describePriceRuleTime, formatModelPrice, modelBillingModeLabel, priceRuleTimeIsRestricted } from './model-pricing'

describe('model pricing display', () => {
  it('includes cache read and cache write as separate configured prices', () => {
    const items = configuredTokenPriceItems({ inputPrice: 1.5, cachedInputPrice: 0.15, cacheWritePrice: 3, outputPrice: 12 })

    expect(items).toEqual([
      { label: '输入', value: 1.5 },
      { label: '缓存读', value: 0.15 },
      { label: '缓存写', value: 3 },
      { label: '输出', value: 12 },
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

describe('price rule time description', () => {
  it('reports unrestricted rules when there is no time block', () => {
    expect(describePriceRuleTime({})).toBe('不限时段')
    expect(describePriceRuleTime(undefined)).toBe('不限时段')
    expect(describePriceRuleTime({ endpoint: '/chat/completions' })).toBe('不限时段')
  })

  it('compresses consecutive weekdays into a range', () => {
    expect(describePriceRuleTime({ time: { weekdays: [1, 2, 3, 4, 5] } })).toBe('周一至周五')
    expect(describePriceRuleTime({ time: { weekdays: [6, 7] } })).toBe('周六至周日')
  })

  it('splits non-consecutive weekdays and keeps input order irrelevant', () => {
    expect(describePriceRuleTime({ time: { weekdays: [3, 1, 5] } })).toBe('周一、周三、周五')
    expect(describePriceRuleTime({ time: { weekdays: [1, 2, 4, 5] } })).toBe('周一至周二、周四至周五')
  })

  it('describes the DeepSeek peak window', () => {
    const conditions = { time: { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '12:00'], ['14:00', '18:00']] } }

    expect(describePriceRuleTime(conditions)).toBe('周一至周五 09:00–12:00、14:00–18:00')
    expect(describePriceRuleConditions(conditions)).toBe('周一至周五 09:00–12:00、14:00–18:00')
  })

  it('falls back to 每天 when only clock ranges are configured', () => {
    expect(describePriceRuleTime({ time: { ranges: [['22:00', '06:00']] } })).toBe('每天 22:00–06:00')
  })

  it('treats all seven weekdays as unrestricted', () => {
    expect(describePriceRuleTime({ time: { weekdays: [1, 2, 3, 4, 5, 6, 7] } })).toBe('不限时段')
    expect(priceRuleTimeIsRestricted({ time: { weekdays: [1, 2, 3, 4, 5, 6, 7] } })).toBe(false)
  })

  it('flags time blocks that actually narrow the window', () => {
    expect(priceRuleTimeIsRestricted({})).toBe(false)
    expect(priceRuleTimeIsRestricted({ time: { tz: 'Asia/Shanghai' } })).toBe(false)
    expect(priceRuleTimeIsRestricted({ time: { weekdays: [1, 2, 3, 4, 5] } })).toBe(true)
    expect(priceRuleTimeIsRestricted({ time: { ranges: [['09:00', '18:00']] } })).toBe(true)
  })

  it('ignores malformed weekday and range values instead of crashing', () => {
    expect(describePriceRuleTime({ time: { weekdays: ['1', 9, 0], ranges: [['09:00'], 'bad', [1, 2]] } })).toBe('每天 1–2')
    expect(priceRuleTimeIsRestricted({ time: { weekdays: 'weekday', ranges: {} } })).toBe(false)
  })

  it('prepends the endpoint condition when present', () => {
    expect(describePriceRuleConditions({ endpoint: '/responses', time: { weekdays: [6, 7] } })).toBe('/responses · 周六至周日')
  })

  it('creates an unrestricted draft with no conditions or rates', () => {
    expect(createPriceRuleDraft()).toEqual({ name: '', priority: 100, currency: 'USD', conditions: {}, rates: {} })
  })
})
