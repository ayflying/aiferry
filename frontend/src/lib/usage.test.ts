import { describe, expect, it } from 'vitest'
import { channelCredentialReference, streamLabel } from './usage'

describe('channelCredentialReference', () => {
  it('combines the channel name with its credential ordinal', () => {
    expect(channelCredentialReference('宝塔', 1)).toBe('宝塔 #1')
    expect(channelCredentialReference('', 0)).toBe('已删除渠道 #未记录')
  })

  it('有备注时用备注顶替序号', () => {
    expect(channelCredentialReference('宝塔', 3, '生产主密钥')).toBe('宝塔 生产主密钥')
    expect(channelCredentialReference('宝塔', 0, '备用')).toBe('宝塔 备用')
  })

  it('备注为空白或只有空格时仍用序号', () => {
    expect(channelCredentialReference('宝塔', 2, '')).toBe('宝塔 #2')
    expect(channelCredentialReference('宝塔', 2, '   ')).toBe('宝塔 #2')
    expect(channelCredentialReference('宝塔', 2, undefined)).toBe('宝塔 #2')
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
