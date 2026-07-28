import { describe, expect, it } from 'vitest'
import { channelCredentialLabel } from './usage'

describe('channelCredentialLabel', () => {
  it('uses the stable credential ordinal instead of its database ID', () => {
    expect(channelCredentialLabel(2)).toBe('密钥 #2')
  })

  it('describes usage records that predate credential tracking', () => {
    expect(channelCredentialLabel(0)).toBe('未记录渠道密钥')
    expect(channelCredentialLabel(undefined)).toBe('未记录渠道密钥')
  })
})
