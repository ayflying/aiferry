import type { UsageLog } from '../api/types'

/**
 * 用量列表的列布局决策。
 *
 * 背景：用量列表最多 10 列（时间/模型/用户/渠道密钥/状态/流式/Token/成本/耗时/详情），
 * 列宽之和远大于 1280、1366 这类笔记本屏幕的可用宽度。早期实现让表格整体超出容器，
 * 右侧「耗时/详情」被裁掉，而横向滚动条落在 20 行表格的最后一行下面，用户既看不到也点不到。
 *
 * 现在的策略（保证「结构上不可能超框」）：
 * 1. 必显列始终保留，首列（时间）固定左侧、末列（详情）固定右侧；
 * 2. 必显列宽度按容器实测宽度等比压缩到 snugWidth，压缩有下限 floorWidth，
 *    因此只要容器宽度 ≥ 全部列宽下限之和，表格就永远不需要横向滚动；
 * 3. 可选列（用户/流式）只在按自然宽度排布后仍有余量时才加入；
 * 4. 列宽常量集中在这里，页面与单测共用同一份数据，避免两处各写一套数字。
 */
export type UsageColumnKey = 'time' | 'model' | 'user' | 'channel' | 'status' | 'stream' | 'tokens' | 'cost' | 'latency' | 'detail'

export interface UsageColumnOptions {
  /** 管理员能看到「用户」列，普通用户看不到。 */
  isAdmin: boolean
  /** 容器实测可用宽度（CSS 像素）。<= 0 表示尚未测量，按自然宽度排布。 */
  availableWidth?: number
}

interface UsageColumnSpec {
  key: UsageColumnKey
  /** 自然宽度：内容完整显示所需的宽度，宽屏下使用。 */
  width: number
  /** 压缩下限：再窄内容会明显截断，低于此值宁可在表格内部横向滚动。 */
  floorWidth: number
}

/**
 * 必显列：身份、归属、结果、用量、成本、耗时与详情入口，任何宽度下都保留。
 * 自然宽度按 12px 单号字体实测内容反推（例如 Token「入 159,506 · 出 184」需 168px）。
 */
const REQUIRED_COLUMNS: UsageColumnSpec[] = [
  { key: 'time', width: 164, floorWidth: 118 },
  { key: 'model', width: 124, floorWidth: 92 },
  { key: 'channel', width: 140, floorWidth: 96 },
  { key: 'status', width: 86, floorWidth: 72 },
  { key: 'tokens', width: 168, floorWidth: 118 },
  { key: 'cost', width: 118, floorWidth: 92 },
  { key: 'latency', width: 124, floorWidth: 88 },
  { key: 'detail', width: 72, floorWidth: 68 },
]

/** 可选列：按自然宽度判断能否加入，先「用户」后「流式」。 */
const OPTIONAL_COLUMNS: UsageColumnSpec[] = [
  { key: 'user', width: 100, floorWidth: 84 },
  { key: 'stream', width: 84, floorWidth: 76 },
]

/** 全部列的自然宽度，供页面兜底与测试对照。 */
export const USAGE_COLUMN_WIDTHS = [...REQUIRED_COLUMNS, ...OPTIONAL_COLUMNS]
  .reduce<Record<string, number>>((widths, column) => ({ ...widths, [column.key]: column.width }), {})

/** 必显列的自然总宽。 */
export const REQUIRED_USAGE_COLUMNS_WIDTH = REQUIRED_COLUMNS.reduce((sum, column) => sum + column.width, 0)

/** 必显列的压缩下限总宽：小于这个宽度才真的需要表格内部横向滚动。 */
export const REQUIRED_USAGE_COLUMNS_MIN_WIDTH = REQUIRED_COLUMNS.reduce((sum, column) => sum + column.floorWidth, 0)

function naturalWidth(columns: UsageColumnSpec[]): number {
  return columns.reduce((sum, column) => sum + column.width, 0)
}

/**
 * 在 selected 中挑选要渲染的列：必显列永远在内，可选列按自然宽度装得下才加入。
 */
function selectColumns(availableWidth: number, isAdmin: boolean): UsageColumnSpec[] {
  const selected = [...REQUIRED_COLUMNS]
  let used = naturalWidth(REQUIRED_COLUMNS)
  for (const column of OPTIONAL_COLUMNS) {
    if (column.key === 'user' && !isAdmin) continue
    if (availableWidth > 0 && used + column.width > availableWidth) continue
    selected.push(column)
    used += column.width
  }
  return selected
}

/**
 * 按容器可用宽度等比压缩列宽，返回每一列的实际渲染宽度。
 *
 * 压不下（容器比压缩下限还窄）时按下限返回并置 overflow=true，
 * 由表格内部横向滚动 + 固定首末列保证关键信息仍可见，整页不会超框。
 */
export function usageColumnLayout(availableWidth: number, options: UsageColumnOptions): {
  columns: UsageColumnKey[]
  widths: Record<string, number>
  overflow: boolean
} {
  const width = options.availableWidth ?? availableWidth
  const selected = selectColumns(width, options.isAdmin)
  const natural = naturalWidth(selected)
  const floor = selected.reduce((sum, column) => sum + column.floorWidth, 0)

  if (width <= 0 || natural <= width) {
    return {
      columns: selected.map((column) => column.key),
      widths: selected.reduce<Record<string, number>>((map, column) => ({ ...map, [column.key]: column.width }), {}),
      overflow: false,
    }
  }
  if (width <= floor) {
    return {
      columns: selected.map((column) => column.key),
      widths: selected.reduce<Record<string, number>>((map, column) => ({ ...map, [column.key]: column.floorWidth }), {}),
      overflow: true,
    }
  }
  // 自然宽度之外按「可压缩空间」等比例分配，保证总和正好等于容器宽度，
  // 既不溢出，也不会因为某一列被压得太狠而内容不可读。
  const room = natural - floor
  const extra = width - floor
  const widths: Record<string, number> = {}
  for (const column of selected) {
    const slack = column.width - column.floorWidth
    widths[column.key] = Math.floor(column.floorWidth + (slack * extra) / room)
  }
  // 取整会留下几像素余量，补给「成本」列（内容最需要横向空间）。
  const assigned = selected.reduce((sum, column) => sum + widths[column.key], 0)
  const remainder = Math.round(width) - assigned
  if (remainder > 0 && widths.cost !== undefined) widths.cost += remainder

  return {
    columns: selected.map((column) => column.key),
    widths,
    overflow: false,
  }
}

/**
 * 根据表格容器的真实可用宽度挑出要渲染的列。
 *
 * 保留该导出供既有调用方与测试使用：等价于 usageColumnLayout 的 columns 集合。
 */
export function visibleUsageColumns(availableWidth: number, options: UsageColumnOptions): Set<UsageColumnKey> {
  return new Set(usageColumnLayout(availableWidth, options).columns)
}

/** 详情列提示文案：成功看计费规则，失败看错误首行，避免为了这一点信息还要打开详情弹窗。 */
export function usageDetailHint(row: UsageLog, priceSummary: string, errorPreview: string): string {
  return row.httpStatus >= 200 && row.httpStatus < 300 ? priceSummary : errorPreview
}
