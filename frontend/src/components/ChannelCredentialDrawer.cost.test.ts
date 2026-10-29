import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel } from '../api/types'
import ChannelCredentialDrawer from './ChannelCredentialDrawer.vue'
import { setDisplayCurrency } from '../lib/format'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), success: vi.fn() }))
vi.mock('../api/client', () => ({ apiGet: api.get, apiPost: api.post, apiPut: vi.fn(), apiDelete: vi.fn() }))
vi.mock('../lib/error', () => ({ showError: vi.fn(), showSuccess: api.success, showWarning: vi.fn() }))

const Slot = defineComponent({ setup: (_, { slots }) => () => h('div', [slots.default?.(), slots.footer?.()]) })
const Button = defineComponent({ props: ['disabled', 'loading'], setup: (props, { slots }) => () => h('button', { disabled: props.disabled || props.loading }, slots.default?.()) })

// 生产实证（2026-10-29）：三把 WorkBuddy 密钥已用 337.93 / 152.21 / 52.34，
// 余额 3,102.07 / 2,147.79 / 2,047.66。页头曾把已用合计 542.48 当成余额显示，
// 还多出一个 USD 币种标记。这条用例锁住修复后的页头显示。
describe('上游密钥页头汇总', () => {
  const channel = {
    id: 35,
    name: 'WorkBuddy',
    costQueryType: 'usage',
    costQueryMode: 'workbuddy_credits',
    costSummaries: [
      // 后端按币种汇总：计数行是 XXX，另外混着一行网关按定价累计的美元费用。
      { currency: 'XXX', usedAmount: 542.48, remainingAmount: 7297.52, usageType: '积分余额', usageUnit: '积分' },
      { currency: 'USD', usedAmount: 0.038418 },
    ],
  } as unknown as Channel

  function mount() {
    return shallowMount(ChannelCredentialDrawer, {
      props: { modelValue: true, channel, loginSupported: false },
      global: { stubs: { ElDrawer: Slot, ElDialog: Slot, ElButton: Button, ElRadioGroup: true } },
    })
  }

  async function querySummary() {
    const wrapper = mount()
    await flushPromises()
    // 汇总来自「查询积分余额」接口返回，与线上路径一致。
    const query = wrapper.findAll('button').find((item) => item.text().includes('查询'))
    if (!query) throw new Error('未找到查询按钮')
    await query.trigger('click')
    await flushPromises()
    return wrapper
  }

  beforeEach(() => {
    setDisplayCurrency('USD')
    api.get.mockResolvedValue([])
    api.post.mockResolvedValue({ summaries: channel.costSummaries, credentials: [] })
  })
  afterEach(() => { vi.restoreAllMocks() })

  it('页头显示余额合计而不是已用合计，也不显示美元标记', async () => {
    const wrapper = await querySummary()
    const summary = wrapper.find('.cost-summary-grid')
    expect(summary.exists()).toBe(true)
    const text = summary.text()
    // 余额合计 3,102.07 + 2,147.79 + 2,047.66 = 7,297.52
    expect(text).toContain('积分余额')
    expect(text).toMatch(/7[,.]?297/)
    // 已用合计 542.48 绝不能出现在「剩余」位置。
    expect(text).not.toMatch(/剩余\s*542/)
    // 计数口径没有货币单位，不能出现 USD。
    expect(text).not.toContain('USD')
    wrapper.unmount()
  })

  it('后端仍返回 usage 时，剩余仍取余额字段', async () => {
    // 防御性回归：即便某个后端版本把「已用」写进 usage，前端也不能拿它当剩余。
    api.post.mockResolvedValue({
      summaries: [{ currency: 'XXX', usedAmount: 542.48, remainingAmount: 7297.52, usage: 542.48, usageType: '积分余额', usageUnit: '积分' }],
      credentials: [],
    })
    const wrapper = await querySummary()
    const text = wrapper.find('.cost-summary-grid').text()
    expect(text).toMatch(/7[,.]?297/)
    expect(text).not.toMatch(/剩余\s*542/)
    wrapper.unmount()
  })
})
