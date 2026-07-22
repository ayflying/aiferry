import { describe, expect, it } from 'vitest'

import { channelNameForID, channelStatusLabel, isChannelEnabled } from './channelDisplay'
import { channelTypeCostLabel } from './channelTypeDisplay'

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
    expect(channelTypeCostLabel({ config: { costs: { adapter: 'custom_adapter' } } } as never)).toBe('custom_adapter')
  })
})
