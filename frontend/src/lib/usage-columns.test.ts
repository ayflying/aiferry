import { describe, expect, it } from 'vitest'
import type { UsageLog } from '../api/types'
import { REQUIRED_USAGE_COLUMNS_MIN_WIDTH, REQUIRED_USAGE_COLUMNS_WIDTH, USAGE_COLUMN_WIDTHS, usageColumnLayout, usageDetailHint, visibleUsageColumns, type UsageColumnKey } from './usage-columns'

function usageRow(overrides: Partial<UsageLog> = {}): UsageLog {
  return {
    id: 1,
    requestId: 'req-1',
    userId: 1,
    userName: '安阳',
    apiKeyName: 'dsh',
    channelName: 'workbuddy',
    channelCredentialIndex: 1,
    endpoint: '/v1/messages',
    upstreamEndpoint: '/v1/messages',
    protocolConversion: 'none',
    requestedModel: 'gpt-6.1-sol',
    upstreamModel: 'gpt-6.1-sol',
    reasoningEffort: 'default',
    httpStatus: 200,
    isStream: 1,
    durationMs: 7300,
    attempts: 1,
    errorMessage: '',
    createdAt: '2026-10-31T08:08:42Z',
    ...overrides,
  }
}

const REQUIRED_KEYS: UsageColumnKey[] = ['time', 'model', 'channel', 'status', 'tokens', 'cost', 'latency', 'detail']
const sumOf = (widths: Record<string, number>, keys: UsageColumnKey[]) => keys.reduce((total, key) => total + (widths[key] ?? 0), 0)

describe('usage columns', () => {
  it('宽屏（内容区 1648）保留全部列且使用自然宽度', () => {
    const layout = usageColumnLayout(1648, { isAdmin: true })
    expect([...layout.columns].sort()).toEqual([...REQUIRED_KEYS, 'user', 'stream'].sort())
    expect(layout.overflow).toBe(false)
    expect(sumOf(layout.widths, layout.columns)).toBeLessThanOrEqual(1648)
  })

  it('1440 宽度只补「用户」列', () => {
    const columns = visibleUsageColumns(1100, { isAdmin: true })
    expect(columns.has('user')).toBe(true)
    expect(columns.has('stream')).toBe(false)
  })

  it('1366 宽度放不下可选列时只保留必显列', () => {
    const columns = visibleUsageColumns(1040, { isAdmin: true })
    expect([...columns].sort()).toEqual([...REQUIRED_KEYS].sort())
  })

  it('普通用户没有「用户」列，剩余宽度会留给「流式」列', () => {
    const columns = visibleUsageColumns(1180, { isAdmin: false })
    expect(columns.has('user')).toBe(false)
    expect(columns.has('stream')).toBe(true)
  })

  it('任意容器宽度下，列宽总和不大于容器宽度（结构上不可能超框）', () => {
    for (const width of [600, 700, 752, 800, 900, 996, 1024, 1100, 1200, 1366, 1648, 2000]) {
      const layout = usageColumnLayout(width, { isAdmin: true })
      const used = sumOf(layout.widths, layout.columns)
      // 压不下时按下限排布并允许表格内部滚动，这是唯一允许「超出」的情形。
      const expected = layout.overflow ? REQUIRED_USAGE_COLUMNS_MIN_WIDTH : width
      expect(used).toBeLessThanOrEqual(Math.max(expected, REQUIRED_USAGE_COLUMNS_MIN_WIDTH))
      if (!layout.overflow) expect(used).toBeLessThanOrEqual(width)
      for (const key of layout.columns) expect(layout.widths[key]).toBeGreaterThan(0)
    }
  })

  it('容器比压缩下限还窄时才标记需要内部横向滚动', () => {
    expect(usageColumnLayout(REQUIRED_USAGE_COLUMNS_MIN_WIDTH + 40, { isAdmin: true }).overflow).toBe(false)
    expect(usageColumnLayout(REQUIRED_USAGE_COLUMNS_MIN_WIDTH - 40, { isAdmin: true }).overflow).toBe(true)
  })

  it('未测量（宽度为 0）时按自然宽度渲染且不溢出', () => {
    const layout = usageColumnLayout(0, { isAdmin: true })
    expect(layout.overflow).toBe(false)
    expect(layout.widths.tokens).toBe(USAGE_COLUMN_WIDTHS.tokens)
  })

  it('压缩时保持顺序与完整性：必显列一个不少，成本列拿到取整余量', () => {
    const layout = usageColumnLayout(900, { isAdmin: true })
    expect(layout.columns).toEqual(expect.arrayContaining(REQUIRED_KEYS))
    expect(sumOf(layout.widths, REQUIRED_KEYS)).toBe(900)
  })

  it('必显列自然总宽大于压缩下限总宽，说明压缩策略有实际空间', () => {
    expect(REQUIRED_USAGE_COLUMNS_WIDTH).toBeGreaterThan(REQUIRED_USAGE_COLUMNS_MIN_WIDTH)
    const sum = REQUIRED_KEYS.reduce((total, key) => total + USAGE_COLUMN_WIDTHS[key], 0)
    expect(REQUIRED_USAGE_COLUMNS_WIDTH).toBe(sum)
  })

  it('详情列提示按成功与失败区分展示内容', () => {
    expect(usageDetailHint(usageRow(), '标准 · $0.3/M', '上游 502')).toBe('标准 · $0.3/M')
    expect(usageDetailHint(usageRow({ httpStatus: 502 }), '标准 · $0.3/M', '上游 502')).toBe('上游 502')
  })
})
