import { describe, expect, it } from 'vitest'
import {
  closedWindowPayload,
  closedWindowsFromModels,
  createClosedWindow,
  describeTimeWindow,
  formatTimeWindowWeekdays,
  readTimeWindow,
  timeWindowIsEmpty,
  toTimeWindow,
} from './time-window'

describe('time window helpers', () => {
  it('falls back to the Beijing timezone when tz is missing or blank', () => {
    expect(toTimeWindow({ weekdays: [1] })).toEqual({ tz: 'Asia/Shanghai', weekdays: [1], ranges: [] })
    expect(toTimeWindow(undefined)).toEqual({ tz: 'Asia/Shanghai', weekdays: [], ranges: [] })
    expect(toTimeWindow({ tz: '  UTC  ' })).toEqual({ tz: 'UTC', weekdays: [], ranges: [] })
  })

  it('ignores malformed weekdays and ranges', () => {
    const window = toTimeWindow({ weekdays: ['1', 9, 0, 3, 3], ranges: [['09:00'], 'bad', ['09:00', '12:00']] })
    expect(window.weekdays).toEqual([3])
    expect(window.ranges).toEqual([['09:00', '12:00']])
  })

  it('treats a window without weekday limit and ranges as unconfigured', () => {
    expect(readTimeWindow(undefined)).toBeNull()
    expect(readTimeWindow('not-an-object')).toBeNull()
    expect(readTimeWindow({ tz: 'UTC' })).toBeNull()
    expect(readTimeWindow({ weekdays: [1, 2, 3, 4, 5, 6, 7] })).toBeNull()
    expect(timeWindowIsEmpty({ tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5, 6, 7], ranges: [] })).toBe(true)
    expect(timeWindowIsEmpty(createClosedWindow())).toBe(false)
  })

  it('keeps a window that restricts weekdays or clock ranges', () => {
    expect(readTimeWindow({ weekdays: [6, 7] })).toEqual({ tz: 'Asia/Shanghai', weekdays: [6, 7], ranges: [] })
    expect(readTimeWindow({ ranges: [['22:00', '06:00']] })).toEqual({ tz: 'Asia/Shanghai', weekdays: [], ranges: [['22:00', '06:00']] })
  })

  it('compresses consecutive weekdays into readable segments', () => {
    expect(formatTimeWindowWeekdays([1, 2, 3, 4, 5])).toBe('周一至周五')
    expect(formatTimeWindowWeekdays([6, 7])).toBe('周六至周日')
    expect(formatTimeWindowWeekdays([3, 1, 5])).toBe('周一、周三、周五')
    expect(formatTimeWindowWeekdays([1, 2, 4, 5])).toBe('周一至周二、周四至周五')
    expect(formatTimeWindowWeekdays([1, 2, 3, 4, 5, 6, 7])).toBe('')
    expect(formatTimeWindowWeekdays([])).toBe('')
  })

  it('renders a readable summary', () => {
    expect(describeTimeWindow(null)).toBe('不限时段')
    expect(describeTimeWindow({ weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '12:00'], ['14:00', '18:00']] }))
      .toBe('周一至周五 09:00–12:00、14:00–18:00')
    expect(describeTimeWindow({ ranges: [['22:00', '06:00']] })).toBe('每天 22:00–06:00')
    expect(describeTimeWindow({ weekdays: [6, 7] })).toBe('周六至周日')
    // 起止相同在后端语义里就是「全天」。
    expect(describeTimeWindow({ ranges: [['00:00', '00:00']] })).toBe('每天 全天')
  })

  it('restores configured windows from channel models and skips unconfigured ones', () => {
    const windows = closedWindowsFromModels([
      { publicName: 'deepseek-flash', closedWindow: { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '12:00']] } },
      { publicName: 'kimi-k2', closedWindow: null },
      { publicName: 'glm-5', closedWindow: { tz: 'UTC', weekdays: [1, 2, 3, 4, 5, 6, 7], ranges: [] } },
    ])
    expect(Object.keys(windows)).toEqual(['deepseek-flash'])
    expect(windows['deepseek-flash'].ranges).toEqual([['09:00', '12:00']])
  })

  it('omits the payload field for untouched models and clears it for emptied windows', () => {
    const windows = {
      'deepseek-flash': { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '12:00']] as Array<[string, string]> },
      'kimi-k2': { tz: '', weekdays: [], ranges: [] as Array<[string, string]> },
    }
    // 没配过的模型不带该字段：后端保持库里原值。
    expect(closedWindowPayload(windows, 'glm-5')).toBeUndefined()
    expect(closedWindowPayload(windows, 'deepseek-flash')).toEqual({
      tz: 'Asia/Shanghai',
      weekdays: [1, 2, 3, 4, 5],
      ranges: [['09:00', '12:00']],
    })
    // 显式清空：提交空时间窗，后端归一成空串并清列。
    expect(closedWindowPayload(windows, 'kimi-k2')).toEqual({ tz: 'Asia/Shanghai', weekdays: [], ranges: [] })
  })
})
