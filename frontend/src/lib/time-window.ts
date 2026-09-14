import type { TimeWindow } from '../api/types'

/** 未显式指定时区时使用的口径：容器本地时区通常是 UTC，按北京时间判定才不会整体偏移 8 小时。 */
export const defaultTimeWindowTimezone = 'Asia/Shanghai'

const weekdayLabels = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

export const timeWindowWeekdays = weekdayLabels.map((label, index) => ({ value: index + 1, label }))

export function createTimeWindow(): TimeWindow {
  return { tz: defaultTimeWindowTimezone, weekdays: [], ranges: [] }
}

/**
 * 把任意来源（后端返回、JSON 字段）的值收敛成编辑器可用的时间窗。
 * 永远返回对象：字段缺失补默认值，非法值忽略，便于表单直接回填。
 */
export function toTimeWindow(value: unknown): TimeWindow {
  const record = value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {}
  const tz = typeof record.tz === 'string' && record.tz.trim() ? record.tz.trim() : defaultTimeWindowTimezone
  return { tz, weekdays: readWeekdays(record), ranges: readClockPairs(record) }
}

/** 读取已配置的时间窗；只有时区、没有星期与时段时视为未配置，返回 null。 */
export function readTimeWindow(value: unknown): TimeWindow | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const window = toTimeWindow(value)
  return timeWindowIsEmpty(window) ? null : window
}

/** 判断时间窗是否收窄了范围：没有任何时段、且星期要么没选要么全选，都等价于全天。 */
export function timeWindowIsEmpty(window?: TimeWindow | null): boolean {
  if (!window) return true
  const weekdays = window.weekdays ?? []
  const hasWeekdayLimit = weekdays.length > 0 && weekdays.length < 7
  return !hasWeekdayLimit && !(window.ranges ?? []).length
}

function readWeekdays(record: Record<string, unknown>): number[] {
  if (!Array.isArray(record.weekdays)) return []
  return [...new Set(record.weekdays.filter((item): item is number => typeof item === 'number' && item >= 1 && item <= 7))]
    .sort((left, right) => left - right)
}

function readClockPairs(record: Record<string, unknown>): Array<[string, string]> {
  if (!Array.isArray(record.ranges)) return []
  return record.ranges
    .filter((item): item is unknown[] => Array.isArray(item) && item.length === 2)
    .map((item) => [String(item[0]), String(item[1])] as [string, string])
}

/** 把星期压缩成「周一至周五」这种可读片段；全选七天或未选择返回空串。 */
export function formatTimeWindowWeekdays(days?: number[]): string {
  const normalized = [...new Set(days ?? [])].filter((item) => item >= 1 && item <= 7).sort((left, right) => left - right)
  if (!normalized.length || normalized.length === 7) return ''
  const segments: string[] = []
  let start = normalized[0]
  let previous = normalized[0]
  for (let index = 1; index <= normalized.length; index += 1) {
    const current = normalized[index]
    if (current === previous + 1) {
      previous = current
      continue
    }
    segments.push(start === previous ? weekdayLabels[start - 1] : `${weekdayLabels[start - 1]}至${weekdayLabels[previous - 1]}`)
    if (current === undefined) break
    start = current
    previous = current
  }
  return segments.join('、')
}

function formatTimeWindowRanges(ranges?: Array<[string, string]>): string {
  return (ranges ?? [])
    .filter((item) => Array.isArray(item) && item.length === 2)
    .map(([start, end]) => (start === end ? '全天' : `${start}–${end}`))
    .join('、')
}

/** 生成时间窗摘要：如「周一至周五 09:00–12:00、14:00–18:00」。 */
export function describeTimeWindow(window?: TimeWindow | null): string {
  if (!window) return '不限时段'
  const weekdays = formatTimeWindowWeekdays(window.weekdays)
  const ranges = formatTimeWindowRanges(window.ranges)
  if (!ranges) return weekdays || '不限时段'
  return `${weekdays || '每天'} ${ranges}`
}

/** 新建一个已收敛的关闭时段：默认从 09:00 关到 12:00，由使用方再改。 */
export function createClosedWindow(): TimeWindow {
  return { ...createTimeWindow(), ranges: [['09:00', '12:00']] }
}

/**
 * 从渠道模型列表里恢复已配置的关闭时段，作为编辑态初值。
 * 只有真正收窄了范围的才进结果：没配过的模型不出现，保存时也就不会提交该字段。
 */
export function closedWindowsFromModels(models: Array<{ publicName: string; closedWindow?: TimeWindow | null }>): Record<string, TimeWindow> {
  const result: Record<string, TimeWindow> = {}
  for (const model of models) {
    const window = readTimeWindow(model.closedWindow)
    if (window) result[model.publicName] = window
  }
  return result
}

/**
 * 生成请求负载里的关闭时段。
 * 返回 undefined 表示不提交该字段（后端保持原值）；返回空时间窗表示清除已配置的时段。
 */
export function closedWindowPayload(windows: Record<string, TimeWindow>, publicName: string): TimeWindow | undefined {
  const window = windows[publicName]
  if (!window) return undefined
  return {
    tz: window.tz?.trim() || defaultTimeWindowTimezone,
    weekdays: window.weekdays ?? [],
    ranges: window.ranges ?? [],
  }
}

/** 空时间窗：键存在但值不构成时段，提交后等价于清除该模型的关闭配置。 */
export function emptyTimeWindow(): TimeWindow {
  return { tz: '', weekdays: [], ranges: [] }
}

/** 从已保存记录里恢复编辑行：只保留真正收窄了范围的时段，没配过的不出现。 */
export function windowRowsFromRecord(record: Record<string, TimeWindow>): Array<{ publicName: string; window: TimeWindow }> {
  return Object.entries(record)
    .filter(([, window]) => !timeWindowIsEmpty(window))
    .map(([publicName, window]) => ({ publicName, window }))
}

/**
 * 把编辑行合成为提交负载。键存在表示本次提交该字段（空时间窗即清除），
 * 键不存在表示后端保持原值——所以被移除的既有键必须补一条空时间窗，
 * 直接删键会被当成「保持原值」，清除就不生效。
 */
export function windowRowsToRecord(
  rows: Array<{ publicName: string; window: TimeWindow }>,
  clearedNames: Iterable<string> = [],
): Record<string, TimeWindow> {
  const payload: Record<string, TimeWindow> = {}
  for (const row of rows) {
    const name = row.publicName.trim()
    if (name) payload[name] = row.window
  }
  for (const name of clearedNames) {
    if (name && !(name in payload)) payload[name] = emptyTimeWindow()
  }
  return payload
}