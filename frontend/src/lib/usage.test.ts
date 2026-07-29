import { describe, expect, it } from 'vitest'
import { channelCredentialReference } from './usage'

describe('channelCredentialReference', () => {
  it('combines the channel name with its credential ordinal', () => {
    expect(channelCredentialReference('宝塔', 1)).toBe('宝塔 #1')
    expect(channelCredentialReference('', 0)).toBe('已删除渠道 #未记录')
  })
})
