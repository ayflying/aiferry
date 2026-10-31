import type { UsageLog } from '../api/types'

/**
 * 用量列表的列布局决策。
 *
 * 背景：用量列表一共 10 列（时间/模型/用户/渠道密钥/状态/流式/Token/成本/耗时/详情），
 * 列宽之和远大于 1280、1366 这类笔记本屏幕的可用宽度。早期实现让表格整体超出容器，
 * 右侧「耗时/详情」被裁掉，而横向滚动条落在 20 行表格的最后一行下面，用户既看不到也点不到。
 *
 * 现在的策略：
 * 1. 必显列始终保留，并且首列（时间）固定左侧、末列（详情）固定右侧，横向滚动时关键信息不丢；
 * 2. 可选列（用户/流式）按「剩余宽度够不够」逐个加入，窄屏自动减列，而不是整页超框；
 * 3. 列宽常量集中在这里，页面与单测共用同一份数据，避免两处各写一套数字。
 */
export type UsageColumnKey = 'time' | 'model' | 'user' | 'channel' | 'status' | 'stream' | 'tokens' | 'cost' | 'latency' | 'detail'

export interface UsageColumnOptions {
  /** 管理员能看到「用户」列，普通用户看不到。 */
  isAdmin: boolean
}

interface UsageColumnSpec {
  key: UsageColumnKey
  minWidth: number
}

/**
 * 必显列：身份、归属、结果、用量、成本、耗时与详情入口，任何宽度下都保留。
 * 宽度按 12px 单号字体实测内容反推（例如 Token「入 159,506 · 出 184」需 168px），
 * 合计 996px，正好落在 1280 屏幕的可用宽度（约 1006px）之内。
 */
const REQUIRED_COLUMNS: UsageColumnSpec[] = [
  { key: 'time', minWidth: 164 },
  { key: 'model', minWidth: 124 },
  { key: 'channel', minWidth: 140 },
  { key: 'status', minWidth: 86 },
  { key: 'tokens', minWidth: 168 },
  { key: 'cost', minWidth: 118 },
  { key: 'latency', minWidth: 124 },
  { key: 'detail', minWidth: 72 },
]

/** 可选列：剩余宽度允许时按顺序加入，先「用户」后「流式」。 */
const OPTIONAL_COLUMNS: UsageColumnSpec[] = [
  { key: 'user', minWidth: 100 },
  { key: 'stream', minWidth: 84 },
]

export const USAGE_COLUMN_WIDTHS = [...REQUIRED_COLUMNS, ...OPTIONAL_COLUMNS]
  .reduce<Record<string, number>>((widths, column) => ({ ...widths, [column.key]: column.minWidth }), {})

/** 必显列的最低总宽：容器比它还窄时，表格只能内部横向滚动。 */
export const REQUIRED_USAGE_COLUMNS_WIDTH = REQUIRED_COLUMNS.reduce((sum, column) => sum + column.minWidth, 0)

/**
 * 根据表格容器的真实可用宽度挑出要渲染的列。
 *
 * 必显列永远返回；可选列只在「已选列宽 + 该列宽 ≤ 可用宽度」时加入，
 * 所以窄屏自动减列、宽屏补齐全部列。返回集合，页面用 `has(key)` 判定。
 */
export function visibleUsageColumns(availableWidth: number, options: UsageColumnOptions): Set<UsageColumnKey> {
  const visible = new Set<UsageColumnKey>(REQUIRED_COLUMNS.map((column) => column.key))
  let used = REQUIRED_USAGE_COLUMNS_WIDTH
  for (const column of OPTIONAL_COLUMNS) {
    if (column.key === 'user' && !options.isAdmin) continue
    if (used + column.minWidth > availableWidth) continue
    visible.add(column.key)
    used += column.minWidth
  }
  return visible
}

/** 详情列提示文案：成功看计费规则，失败看错误首行，避免为了这一点信息还要打开详情弹窗。 */
export function usageDetailHint(row: UsageLog, priceSummary: string, errorPreview: string): string {
  return row.httpStatus >= 200 && row.httpStatus < 300 ? priceSummary : errorPreview
}
