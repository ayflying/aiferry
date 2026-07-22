import { describe, expect, it } from 'vitest'
import dayjs from 'dayjs'
import { customDashboardPeriod, dashboardDatesForPeriod, dashboardPeriodQuery } from './dashboard-range'

describe('dashboard date range', () => {
  it('converts a preset into a complete local date range', () => {
    expect(dashboardDatesForPeriod({ kind: 'preset', days: 7 }, dayjs('2026-07-20T15:30:00+08:00'))).toEqual(['2026-07-14', '2026-07-20'])
  })

  it('serializes custom ranges without a days parameter', () => {
    const period = customDashboardPeriod(['2026-07-01', '2026-07-20'])
    expect(period).toEqual({ kind: 'custom', startAt: '2026-07-01', endAt: '2026-07-20' })
    if (typeof period !== 'string') expect(dashboardPeriodQuery(period)).toEqual({ startAt: '2026-07-01', endAt: '2026-07-20' })
  })

  it('rejects a range longer than 90 days', () => {
    expect(customDashboardPeriod(['2026-04-21', '2026-07-20'])).toBe('自定义时间范围最多 90 天')
  })
})
