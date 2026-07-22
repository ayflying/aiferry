import dayjs from 'dayjs'

export const dashboardPresetDays = [7, 30, 90] as const
export const maxDashboardRangeDays = 90

export type DashboardPeriod =
  | { kind: 'preset'; days: typeof dashboardPresetDays[number] }
  | { kind: 'custom'; startAt: string; endAt: string }

export function dashboardPeriodQuery(period: DashboardPeriod): Record<string, string | number> {
  return period.kind === 'preset'
    ? { days: period.days }
    : { startAt: period.startAt, endAt: period.endAt }
}

export function dashboardDatesForPeriod(period: DashboardPeriod, now = dayjs()): [string, string] {
  if (period.kind === 'custom') return [period.startAt, period.endAt]
  const end = now.startOf('day')
  return [end.subtract(period.days - 1, 'day').format('YYYY-MM-DD'), end.format('YYYY-MM-DD')]
}

export function customDashboardPeriod(dates: [string, string]): DashboardPeriod | string {
  const [startAt, endAt] = dates
  const start = dayjs(startAt).startOf('day')
  const end = dayjs(endAt).startOf('day')
  if (!start.isValid() || !end.isValid()) return '请选择有效的开始日期和结束日期'
  if (end.isBefore(start)) return '结束日期不能早于开始日期'
  if (end.diff(start, 'day') + 1 > maxDashboardRangeDays) return '自定义时间范围最多 90 天'
  return { kind: 'custom', startAt: start.format('YYYY-MM-DD'), endAt: end.format('YYYY-MM-DD') }
}
