import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

// UsageView 在挂载时会调 apiGet 拉列表；这里 mock 掉客户端，
// 让组件拿到「后端真实返回的字段形状」，然后直接断言页面实际调用的成本文案函数。
const usagePage = {
  items: [] as unknown[],
  summary: { requests: 0, estimatedCost: 0 },
  startAt: '2026-11-01T00:00:00Z',
  endAt: '2026-11-03T00:00:00Z',
  total: 0,
  page: 1,
  pageSize: 20,
}

vi.mock('../api/client', () => ({
  apiGet: vi.fn(async (path: string) => (path === '/usage' ? usagePage : {})),
}))

vi.mock('../stores/app', () => ({
  useAppStore: () => ({ apiKeys: [], channels: [], loadAPIKeys: vi.fn(), loadChannels: vi.fn() }),
}))
vi.mock('../stores/auth', () => ({
  useAuthStore: () => ({ user: { isAdmin: true, id: 1 } }),
}))

function row(overrides: Record<string, unknown>) {
  return {
    id: 1,
    requestId: 'req',
    userId: 1,
    userName: 'u',
    apiKeyName: 'k',
    channelName: 'c',
    channelCredentialIndex: 1,
    endpoint: '/v1/chat/completions',
    upstreamEndpoint: '/chat/completions',
    protocolConversion: '',
    requestedModel: 'verify-model',
    upstreamModel: 'mock-gpt',
    reasoningEffort: '',
    httpStatus: 200,
    isStream: 0,
    durationMs: 1000,
    attempts: 1,
    errorMessage: '',
    createdAt: '2026-11-03T10:00:00Z',
    ...overrides,
  }
}

// 后端真实落库形状：502 + 标记价 0.00000001 + failed 快照 total=6
const failedWithSnapshot = row({
  httpStatus: 502,
  estimatedCost: 0.00000001,
  inputTokens: 1_000_000,
  outputTokens: 500_000,
  totalTokens: 1_500_000,
  billingDetails: {
    billingMode: 'failed',
    currency: 'USD',
    charged: false,
    items: [
      { type: 'input', quantity: 1_000_000, unit: 'per_million_tokens', unitPrice: '2', priceSource: 'input', amount: '2' },
      { type: 'output', quantity: 500_000, unit: 'per_million_tokens', unitPrice: '8', priceSource: 'output', amount: '4' },
    ],
    subtotal: '6',
    total: '6.00000000',
  },
})

const failedWithoutSnapshot = row({ httpStatus: 502, estimatedCost: null, inputTokens: 0, outputTokens: 0, totalTokens: 0 })

describe('usage page cost labels', () => {
  it('失败行：有快照显示真实金额并标记已计费，无快照显示 $0.00，都不出现未定价', async () => {
    usagePage.items = [failedWithSnapshot, failedWithoutSnapshot]
    usagePage.total = 2
    const { default: UsageView } = await import('../views/UsageView.vue')
    const wrapper = shallowMount(UsageView, {
      global: { stubs: { UsageDetailDialog: true }, directives: { loading: {} } },
    })
    await new Promise((resolve) => setTimeout(resolve, 60))

    // 页面的表格/移动端模板都调用这两个函数；断言它们就是断言用户看到的文案。
    const vm = wrapper.vm as unknown as {
      costLabel: (r: unknown) => string
      costBadge: (r: unknown) => string
    }
    expect(vm.costLabel(failedWithSnapshot)).toBe('$6.00')
    expect(vm.costBadge(failedWithSnapshot)).toBe('已计费')
    expect(vm.costLabel(failedWithoutSnapshot)).toBe('$0.00')
    expect(vm.costBadge(failedWithoutSnapshot)).toBe('')

    for (const value of [vm.costLabel(failedWithSnapshot), vm.costLabel(failedWithoutSnapshot)]) {
      expect(value).not.toContain('未定价')
    }
    wrapper.unmount()
  }, 20000)
})
