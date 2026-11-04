import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UsageDetailDialog from './UsageDetailDialog.vue'
import type { UsageLog } from '../api/types'

describe('失败请求详情的费用与费率', () => {
  it('旧非零快照也显示零费用，套餐单价不受影响', () => {
    const usage = {
      httpStatus: 502, errorMessage: 'upstream failed',
      billingDetails: {
        billingMode: 'rules', currency: 'USD', charged: false, reconstructed: true,
        rule: { id: 1, name: '高峰时段', priority: 1, source: 'manual', conditions: '{}' },
        items: [{ type: 'input', quantity: 0, unit: 'per_million_tokens', unitPrice: '2', amount: '6' }],
        subtotal: '6', total: '6',
      },
    } as UsageLog
    const wrapper = shallowMount(UsageDetailDialog, { props: { modelValue: false, usage } })
    const vm = wrapper.vm as unknown as {
      billingItems: { amount: string; unitPrice: string }[]
      billingChargeLabel: string
      billingSourceLabel: string
      billingSummary: () => string[]
    }
    expect(vm.billingItems[0]).toMatchObject({ amount: '0', unitPrice: '2' })
    expect(vm.billingChargeLabel).toBe('失败请求，费用为 0，未扣款')
    expect(vm.billingSourceLabel).toContain('非原始快照')
    expect(vm.billingSummary().at(-1)).toBe('$0.00')
    expect(usage.billingDetails?.items[0]?.amount).toBe('6')
    wrapper.unmount()
  })
})
