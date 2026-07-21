import { describe, expect, it } from 'vitest'
import type { Channel } from '../api/types'
import { displayedRoutes, isChannelRoutable, routeDisplayLimit } from './route-display'

function channel(id: number, weight: number, overrides: Partial<Channel> = {}): Channel {
  return {
    id,
    name: `channel-${id}`,
    status: 1,
    autoDisabled: false,
    credentialsUnavailable: false,
    weight,
    priority: 0,
    ...overrides,
  } as Channel
}

describe('航线展示', () => {
  it('按权重降序展示最多六条，且不改变原数组', () => {
    const channels = [
      channel(1, 10),
      channel(2, 80),
      channel(3, 30),
      channel(4, 60),
      channel(5, 50),
      channel(6, 40),
      channel(7, 70),
    ]

    expect(displayedRoutes(channels).map((item) => item.id)).toEqual([2, 7, 4, 5, 6, 3])
    expect(displayedRoutes(channels)).toHaveLength(routeDisplayLimit)
    expect(channels.map((item) => item.id)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('仅将手动启用、未自动禁用且至少有一个可用密钥的渠道判为可路由', () => {
    expect(isChannelRoutable(channel(1, 1))).toBe(true)
    expect(isChannelRoutable(channel(2, 1, { status: 0 }))).toBe(false)
    expect(isChannelRoutable(channel(3, 1, { autoDisabled: true }))).toBe(false)
    expect(isChannelRoutable(channel(4, 1, { credentialsUnavailable: true }))).toBe(false)
  })
})
