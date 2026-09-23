import { describe, expect, it } from 'vitest'
import { buildPriceRuleOverview, configuredTokenPriceItems, createPriceRuleDraft, describePriceRuleConditions, describePriceRuleRates, describePriceRuleTier, priceRuleConditionSummary, describePriceRuleTime, formatModelPrice, modelBillingModeLabel, priceRuleTimeIsRestricted, priceRuleToDraft } from './model-pricing'
import type { PriceRule } from '../api/types'

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

describe('高级规则的可读档位与价格', () => {
  const base: PriceRule = { id: 1, channelModelId: 1, name: '官方价格', source: 'sync', sourceRef: '', priority: 0, currency: 'USD', conditions: { inputTokensAtMost: 272000 }, rates: { inputPerMillion: 2, outputPerMillion: 8, cachedInputPerMillion: 0.2 }, status: 1, updatedAt: '' }
  const peak: PriceRule = { ...base, id: 2, conditions: { inputTokensAtMost: 272000, time: { ranges: [['09:00', '18:00']] } }, rates: { inputPerMillion: 4, outputPerMillion: 16 } }

  it('显示两个长度档与高峰、其他时段，不把兜底说成全天同价', () => {
    expect(describePriceRuleTier(base, [base, peak])).toBe('输入 ≤ 272,000 Token · 其他时段（高峰规则优先）')
    expect(describePriceRuleTier(peak, [base, peak])).toBe('输入 ≤ 272,000 Token · 每天 09:00–18:00')
    const long = { ...base, id: 3, conditions: { inputTokensAtLeast: 272001 } }
    expect(describePriceRuleTier(long, [base, peak, long])).toBe('输入 ≥ 272,001 Token · 不限时段')
    expect(describePriceRuleTier({ ...long, conditions: { inputTokensAtLeast: 100, inputTokensAtMost: 200 } }, [])).toBe('输入 100–200 Token · 不限时段')
  })

  it('把两种长度和两种时段合成一张价格总览，而非四张说明卡', () => {
    const long = { ...base, id: 3, conditions: { inputTokensAtLeast: 272001 }, rates: { inputPerMillion: 3, outputPerMillion: 12 } }
    const longPeak = { ...peak, id: 4, conditions: { inputTokensAtLeast: 272001, time: { ranges: [['09:00', '18:00']] } }, rates: { inputPerMillion: 6, outputPerMillion: 24 } }
    const overview = buildPriceRuleOverview([base, peak, long, longPeak])
    expect(overview?.columns).toEqual(['其他时段（高峰规则优先）', '每天 09:00–18:00'])
    expect(overview?.rows.map((row) => row.tier)).toEqual(['输入 ≤ 272,000 Token', '输入 ≥ 272,001 Token'])
    expect(overview?.rows[0]?.cells.map((cell) => cell?.rates[0])).toEqual(['输入 $2 / 1M Token', '输入 $4 / 1M Token'])
    expect(overview?.rows[1]?.cells.map((cell) => cell?.rates[0])).toEqual(['输入 $3 / 1M Token', '输入 $6 / 1M Token'])
  })

  it('条件超出长度和时段、格子缺失或有重叠时，不拼造矩阵', () => {
    expect(buildPriceRuleOverview([{ ...base, conditions: { endpoint: '/responses' } }])).toBeNull()
    expect(priceRuleConditionSummary({ ...base, conditions: { endpoint: '/responses' } }, [base])).toContain('端点 /responses')
    expect(buildPriceRuleOverview([base, peak, { ...base, id: 3, conditions: { inputTokensAtLeast: 272001 } }])).toBeNull()
    expect(buildPriceRuleOverview([base, { ...base, id: 3 }])).toBeNull()
    expect(buildPriceRuleOverview([base, { ...base, id: 3, conditions: { inputTokensAtLeast: 272000 } }])).toBeNull()
    expect(buildPriceRuleOverview([{ ...base, conditions: { inputTokensAtMost: 100 } }, { ...base, id: 3, conditions: { inputTokensAtLeast: 102 } }])).toBeNull()
    expect(buildPriceRuleOverview([{ ...base, priority: 0 }, { ...peak, priority: -1 }])).toBeNull()
  })

  it('按货币与计价单位展示已配置价格，保留零价、不展示未配置字段', () => {
    expect(describePriceRuleRates(base)).toEqual(['输入 $2 / 1M Token', '输出 $8 / 1M Token', '缓存读取 $0.2 / 1M Token'])
    expect(describePriceRuleRates({ ...base, currency: 'CNY', rates: { request: 0 } })).toEqual(['每请求 ¥0 / 请求'])
  })
})

describe('price rule draft hydration', () => {
  const rule: PriceRule = {
    id: 7,
    channelModelId: 3,
    name: 'Chat 高峰价',
    source: 'sync',
    sourceRef: '/chat/completions',
    priority: 20,
    currency: 'CNY',
    conditions: { endpoint: '/chat/completions', time: { weekdays: [1, 2, 3] } },
    rates: { inputPerMillion: 4, outputPerMillion: 12 },
    status: 1,
    updatedAt: '2026-09-14T00:00:00Z',
  }

  it('carries every editable field into the draft', () => {
    expect(priceRuleToDraft(rule)).toEqual({
      name: 'Chat 高峰价',
      priority: 20,
      currency: 'CNY',
      conditions: { endpoint: '/chat/completions', time: { weekdays: [1, 2, 3] } },
      rates: { inputPerMillion: 4, outputPerMillion: 12 },
    })
  })

  it('copies conditions and rates instead of aliasing the rule', () => {
    const draft = priceRuleToDraft(rule)
    draft.conditions['endpoint'] = '/responses'
    draft.rates['inputPerMillion'] = 99

    // 编辑器与规则列表共用一份数据，别名会让列表展示跟着草稿一起变。
    expect(rule.conditions['endpoint']).toBe('/chat/completions')
    expect(rule.rates['inputPerMillion']).toBe(4)
  })

  it('falls back to safe defaults for missing name, priority, currency and maps', () => {
    const broken = { ...rule, name: undefined, priority: undefined, currency: '  ', conditions: undefined, rates: undefined } as unknown as PriceRule

    expect(priceRuleToDraft(broken)).toEqual({ name: '', priority: 100, currency: 'USD', conditions: {}, rates: {} })
  })
})
