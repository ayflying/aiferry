import { describe, expect, it } from 'vitest'

import { formatTimeZoneUTCOffset, timeZoneOptionGroups } from './time-zones'

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

  it('shows each option with its current UTC offset', () => {
    expect(options.every((option) => /\(UTC[+-]\d{2}:\d{2}\)$/.test(option.label))).toBe(true)
    expect(options).toContainEqual({ label: '中国标准时间 (UTC+08:00)', value: 'Asia/Shanghai' })
  })

  it('uses the offset for the selected date, including daylight saving time', () => {
    expect(formatTimeZoneUTCOffset('Asia/Shanghai', new Date('2026-07-23T00:00:00Z'))).toBe('UTC+08:00')
    expect(formatTimeZoneUTCOffset('Europe/London', new Date('2026-01-23T00:00:00Z'))).toBe('UTC+00:00')
    expect(formatTimeZoneUTCOffset('Europe/London', new Date('2026-07-23T00:00:00Z'))).toBe('UTC+01:00')
  })
})
