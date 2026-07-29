import { describe, expect, it } from 'vitest'
import { channelCredentialLabel, channelCredentialReference } from './usage'

describe('channelCredentialLabel', () => {
  it('uses the stable credential ordinal instead of its database ID', () => {
    expect(channelCredentialLabel(2)).toBe('密钥 #2')
  })

  it('describes usage records that predate credential tracking', () => {
    expect(channelCredentialLabel(0)).toBe('未记录渠道密钥')
    expect(channelCredentialLabel(undefined)).toBe('未记录渠道密钥')
  })

  it('combines the channel name with its credential ordinal', () => {
    expect(channelCredentialReference('宝塔', 1)).toBe('宝塔 #1')
    expect(channelCredentialReference('', 0)).toBe('已删除渠道 #未记录')
  })
})
