import { describe, expect, it } from 'vitest'

import { channelNameForID, channelStatusLabel, isChannelEnabled } from './channelDisplay'
import { channelQueryValueLabel, channelTypeCostLabel, channelTypeQueryLabel, isUsageAdapter, isUsageMode } from './channelTypeDisplay'

describe('channel display helpers', () => {
  it('derives route availability from every blocking status', () => {
    expect(isChannelEnabled({ status: 1, autoDisabled: false, credentialsUnavailable: false })).toBe(true)
    expect(isChannelEnabled({ status: 0, autoDisabled: false, credentialsUnavailable: false })).toBe(false)
    expect(isChannelEnabled({ status: 1, autoDisabled: true, credentialsUnavailable: false })).toBe(false)
    expect(isChannelEnabled({ status: 1, autoDisabled: false, credentialsUnavailable: true })).toBe(false)
  })

  it('keeps the existing status labels and channel fallback name', () => {
    expect(channelStatusLabel({ status: 1, autoDisabled: true, credentialsUnavailable: false })).toBe('渠道自动禁用')
    expect(channelStatusLabel({ status: 0, autoDisabled: false, credentialsUnavailable: false })).toBe('手动停用')
    expect(channelStatusLabel({ status: 1, autoDisabled: false, credentialsUnavailable: true })).toBe('所有密钥不可用')
    expect(channelNameForID([{ id: 7, name: '主线路' }], 7)).toBe('主线路')
    expect(channelNameForID([], 99)).toBe('#99')
  })

  it('uses a readable label for known and custom cost adapters', () => {
    expect(channelTypeCostLabel({ config: { costs: { adapter: 'newapi_balance' } } } as never)).toBe('NewAPI 余额')
    expect(channelTypeCostLabel({ config: { costs: { adapter: 'siliconflow_balance' } } } as never)).toBe('硅基流动余额')
    expect(channelTypeCostLabel({ config: { costs: { adapter: 'openrouter_credits' } } } as never)).toBe('OpenRouter 余额')
    expect(channelTypeCostLabel({ config: { costs: { adapter: 'custom_adapter' } } } as never)).toBe('custom_adapter')
  })

  it('labels Qiniu estimated costs and legacy usage adapters', () => {
    const qiniuCosts = { config: { costs: { adapter: 'qiniu_costs' } } } as never
    expect(channelTypeCostLabel(qiniuCosts)).toBe('七牛预估费用')
    expect(channelTypeQueryLabel(qiniuCosts)).toBe('费用/余额查询')
    expect(isUsageAdapter(qiniuCosts)).toBe(false)
    expect(channelQueryValueLabel(undefined, 'qiniu_costs')).toBe('预估费用')
    const usage = { config: { costs: { adapter: 'qiniu_usage' } } } as never
    expect(isUsageAdapter(usage)).toBe(true)
    expect(channelTypeQueryLabel({ config: { costs: { adapter: 'newapi_balance' } } } as never)).toBe('费用/余额查询')
    expect(isUsageAdapter({ config: { costs: { adapter: 'sub2api_usage' } } } as never)).toBe(false)
    expect(channelQueryValueLabel(undefined, 'newapi_balance')).toBe('余额')
    expect(channelQueryValueLabel(undefined, 'siliconflow_balance')).toBe('余额')
    expect(channelQueryValueLabel(undefined, 'openrouter_credits')).toBe('余额')
    expect(channelQueryValueLabel(undefined, 'openai_costs')).toBe('费用')
  })

  it('uses an explicit usage value type for custom adapters', () => {
    const usage = { config: { costs: { adapter: 'custom_json', valueType: 'usage' } } } as never
    expect(channelTypeQueryLabel(usage)).toBe('Token/积分用量查询')
    expect(isUsageAdapter(usage)).toBe(true)
    expect(channelQueryValueLabel('usage', 'custom_json')).toBe('用量')
  })

  it('treats WorkBuddy credits as a counter, never as money', () => {
    // workbuddy_credits 回的是积分额度：按钮文案要写「积分余额」，且必须被识别为
    // 计数口径，否则前端会用货币格式化+汇率折算把积分显示成金额（2026-10-27 的诉求）。
    const workbuddy = { config: { costs: { adapter: 'workbuddy_credits' } } } as never
    expect(channelTypeCostLabel(workbuddy)).toBe('积分余额')
    expect(channelTypeQueryLabel(workbuddy)).toBe('积分余额查询')
    expect(isUsageAdapter(workbuddy)).toBe(true)
    // 渠道列表只拿到 costQueryMode（没有 valueType），也必须判成计数口径。
    expect(isUsageMode(undefined, 'workbuddy_credits')).toBe(true)
    expect(channelQueryValueLabel(undefined, 'workbuddy_credits')).toBe('积分余额')
  })
})
