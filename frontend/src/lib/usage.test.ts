import { describe, expect, it } from 'vitest'
import { channelCredentialReference, streamLabel } from './usage'

describe('channelCredentialReference', () => {
  it('combines the channel name with its credential ordinal', () => {
    expect(channelCredentialReference('宝塔', 1)).toBe('宝塔 #1')
    expect(channelCredentialReference('', 0)).toBe('已删除渠道 #未记录')
  })
})

describe('streamLabel', () => {
  it('尝试次数为 1 时只显示基础文案', () => {
    expect(streamLabel({ isStream: 1, attempts: 1 })).toBe('流式')
    expect(streamLabel({ isStream: 0, attempts: 1 }, true)).toBe('非流式响应')
  })

  it('尝试次数超过 1 时追加调用次数', () => {
    expect(streamLabel({ isStream: 1, attempts: 3 })).toBe('流式 - 3')
    expect(streamLabel({ isStream: 0, attempts: 2 })).toBe('非流式 - 2')
    expect(streamLabel({ isStream: 1, attempts: 4 }, true)).toBe('流式响应 - 4')
  })
})
