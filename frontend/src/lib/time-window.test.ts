import { describe, expect, it } from 'vitest'
import {
  closedWindowPayload,
  closedWindowsFromModels,
  createClosedWindow,
  createClosedWindows,
  describeTimeWindow,
  describeTimeWindows,
  formatTimeWindowWeekdays,
  readTimeWindow,
  readTimeWindows,
  timeWindowIsEmpty,
  timeWindowListIsEmpty,
  toTimeWindow,
  windowRowsFromRecord,
  windowRowsToRecord,
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

  it('reads rule lists and stays compatible with the legacy single object', () => {
    // 新版数组：只有真正收窄范围的窗口进结果。
    const windows = readTimeWindows([
      { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '18:00']] },
      { tz: 'Asia/Shanghai', weekdays: [6, 7], ranges: [['00:00', '00:00']] },
      { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5, 6, 7], ranges: [] },
    ])
    expect(windows).toHaveLength(2)
    expect(windows?.[1]?.ranges).toEqual([['00:00', '00:00']])

    // 旧版单对象（存量数据）自动包装成单元素列表。
    expect(readTimeWindows({ weekdays: [1], ranges: [['09:00', '12:00']] })).toEqual([
      { tz: 'Asia/Shanghai', weekdays: [1], ranges: [['09:00', '12:00']] },
    ])

    expect(readTimeWindows(null)).toBeNull()
    expect(readTimeWindows([])).toBeNull()
    expect(readTimeWindows([{ tz: 'UTC' }])).toBeNull()
    expect(timeWindowListIsEmpty([{ weekdays: [1], ranges: [['09:00', '12:00']] }])).toBe(false)
    expect(timeWindowListIsEmpty([{ tz: 'UTC' }])).toBe(true)
    expect(timeWindowListIsEmpty([])).toBe(true)
    expect(createClosedWindows()).toHaveLength(1)
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

  it('joins multiple rules into one summary line', () => {
    expect(describeTimeWindows([
      { weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '18:00']] },
      { weekdays: [6, 7], ranges: [['00:00', '00:00']] },
    ])).toBe('周一至周五 09:00–18:00；周六至周日 全天')
    expect(describeTimeWindows([])).toBe('不限时段')
    expect(describeTimeWindows([{ tz: 'UTC' }])).toBe('未设置时段')
  })

  it('restores configured rule lists from channel models and skips unconfigured ones', () => {
    const windows = closedWindowsFromModels([
      {
        publicName: 'deepseek-flash',
        closedWindows: [
          { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '18:00']] },
          { tz: 'Asia/Shanghai', weekdays: [6, 7], ranges: [['00:00', '00:00']] },
        ],
      },
      { publicName: 'kimi-k2', closedWindows: null },
      { publicName: 'glm-5', closedWindows: [{ tz: 'UTC', weekdays: [1, 2, 3, 4, 5, 6, 7], ranges: [] }] },
    ])
    expect(Object.keys(windows)).toEqual(['deepseek-flash'])
    expect(windows['deepseek-flash']).toHaveLength(2)
    expect(windows['deepseek-flash']?.[1]?.ranges).toEqual([['00:00', '00:00']])
  })

  it('omits the payload field for untouched models and clears it with an empty array', () => {
    const windows = {
      'deepseek-flash': [{ tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '12:00']] as Array<[string, string]> }],
      'kimi-k2': [],
    }
    // 没配过的模型不带该字段：后端保持库里原值。
    expect(closedWindowPayload(windows, 'glm-5')).toBeUndefined()
    expect(closedWindowPayload(windows, 'deepseek-flash')).toEqual([
      { tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5], ranges: [['09:00', '12:00']] },
    ])
    // 显式清空：提交空数组，后端归一成空串并清列。
    expect(closedWindowPayload(windows, 'kimi-k2')).toEqual([])
  })

  it('restores editable rows only for rule lists that really restrict the schedule', () => {
    const rows = windowRowsFromRecord({
      'deepseek-flash': [{ tz: 'Asia/Shanghai', weekdays: [1], ranges: [] }],
      'glm-5': [{ tz: 'Asia/Shanghai', weekdays: [], ranges: [] }],
      'kimi-k2': [{ tz: 'Asia/Shanghai', weekdays: [1, 2, 3, 4, 5, 6, 7], ranges: [] }],
    })
    expect(rows.map((row) => row.publicName)).toEqual(['deepseek-flash'])
  })

  it('drops rows that have no model selected from the payload', () => {
    const payload = windowRowsToRecord([
      { publicName: '   ', windows: createClosedWindows() },
      { publicName: 'deepseek-flash', windows: createClosedWindows() },
    ])
    expect(Object.keys(payload)).toEqual(['deepseek-flash'])
  })

  it('keeps a removed entry as an empty list so clearing survives the round trip', () => {
    // 删除既有行必须留键置空：直接删键会被后端当成「保持原值」，清除就不生效。
    const payload = windowRowsToRecord([], ['glm-5'])
    expect(Object.keys(payload)).toEqual(['glm-5'])
    expect(payload['glm-5']).toEqual([])
    expect(timeWindowListIsEmpty(payload['glm-5'])).toBe(true)
    expect(closedWindowPayload(payload, 'glm-5')).toEqual([])
    // 从未配置过的模型仍然不带该字段，后端保持库里原值。
    expect(closedWindowPayload(payload, 'kimi-k2')).toBeUndefined()
  })

  it('prefers the row value when a model is edited and marked cleared at the same time', () => {
    const payload = windowRowsToRecord([{ publicName: 'glm-5', windows: createClosedWindows() }], ['glm-5'])
    expect(payload['glm-5']?.[0]?.ranges).toEqual([['09:00', '12:00']])
  })

  it('round-trips editable rows through the record helpers', () => {
    const rows = [{ publicName: 'deepseek-flash', windows: createClosedWindows() }]
    expect(windowRowsFromRecord(windowRowsToRecord(rows))).toEqual(rows)
  })
})
