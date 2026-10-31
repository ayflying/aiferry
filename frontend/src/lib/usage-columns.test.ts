import { describe, expect, it } from 'vitest'
import type { UsageLog } from '../api/types'
import { REQUIRED_USAGE_COLUMNS_WIDTH, USAGE_COLUMN_WIDTHS, usageDetailHint, visibleUsageColumns, type UsageColumnKey } from './usage-columns'

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

describe('usage columns', () => {
  it('宽屏（1920 内容区 1648）保留全部列', () => {
    const columns = visibleUsageColumns(1648, { isAdmin: true })
    expect([...columns].sort()).toEqual([...REQUIRED_KEYS, 'user', 'stream'].sort())
  })

  it('1440 宽度只补「用户」列，用户与流式都要看是否真放得下', () => {
    const columns = visibleUsageColumns(1100, { isAdmin: true })
    expect(columns.has('user')).toBe(true)
    expect(columns.has('stream')).toBe(false)
  })

  it('1366 宽度放不下可选列时只保留必显列，且不会整页超框', () => {
    const columns = visibleUsageColumns(1040, { isAdmin: true })
    expect([...columns].sort()).toEqual([...REQUIRED_KEYS].sort())
  })

  it('普通用户没有「用户」列，剩余宽度会留给「流式」列', () => {
    const columns = visibleUsageColumns(1180, { isAdmin: false })
    expect(columns.has('user')).toBe(false)
    expect(columns.has('stream')).toBe(true)
  })

  it('必显列总宽固定，便于判断何时需要内部横向滚动', () => {
    const sum = REQUIRED_KEYS.reduce((total, key) => total + USAGE_COLUMN_WIDTHS[key], 0)
    expect(REQUIRED_USAGE_COLUMNS_WIDTH).toBe(sum)
    // 1024 宽屏幕（侧栏 224 + 页面内边距 48）仍有约 752px 可用，
    // 必显列超出时由固定首末列 + 表格内部横向滚动兜底，不能整页溢出。
    expect(REQUIRED_USAGE_COLUMNS_WIDTH).toBeGreaterThan(752)
    expect(REQUIRED_USAGE_COLUMNS_WIDTH).toBeLessThan(1200)
  })

  it('详情列提示按成功与失败区分展示内容', () => {
    expect(usageDetailHint(usageRow(), '标准 · $0.3/M', '上游 502')).toBe('标准 · $0.3/M')
    expect(usageDetailHint(usageRow({ httpStatus: 502 }), '标准 · $0.3/M', '上游 502')).toBe('上游 502')
  })
})
