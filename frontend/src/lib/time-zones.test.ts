import { describe, expect, it } from 'vitest'

import { timeZoneOptionGroups } from './time-zones'

describe('timeZoneOptionGroups', () => {
  const options = timeZoneOptionGroups.flatMap((group) => group.options)

  it('keeps the default and common global cities selectable', () => {
    const values = options.map((option) => option.value)

    expect(values).toEqual(expect.arrayContaining([
      'Asia/Shanghai', 'Asia/Tokyo', 'Europe/London', 'America/New_York', 'Pacific/Auckland',
    ]))
    expect(values.length).toBeGreaterThan(60)
  })

  it('does not repeat an IANA timezone', () => {
    const values = options.map((option) => option.value)

    expect(new Set(values)).toHaveLength(values.length)
  })
})
