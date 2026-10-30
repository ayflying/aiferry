import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent } from 'vue'

import ChannelModelTestDialog from './ChannelModelTestDialog.vue'

const SlotStub = defineComponent({ template: '<div><slot /></div>' })
const SelectStub = defineComponent({ props: ['modelValue', 'disabled'], emits: ['update:modelValue'], template: '<div><slot /></div>' })
const SwitchStub = defineComponent({ props: ['modelValue', 'disabled'], template: '<span />' })
const OptionStub = defineComponent({ props: ['label', 'value'], template: '<span>{{ label }}</span>' })

describe('channel model test rerank selection', () => {
  it('提供 Rerank 测试选项并禁用流式开关，切回自动检测恢复流式选择', async () => {
    const wrapper = shallowMount(ChannelModelTestDialog, {
      props: { modelValue: true },
      global: {
        stubs: { ElDialog: SlotStub, ElSelect: SelectStub, ElOption: OptionStub, ElSwitch: SwitchStub },
        directives: { loading: {} },
      },
    })
    const endpointSelect = wrapper.findAllComponents(SelectStub)[0]
    expect(wrapper.findAllComponents(OptionStub).some((option) => option.props('value') === 'rerank')).toBe(true)
    expect(wrapper.findComponent(SwitchStub).props('disabled')).toBe(false)
    endpointSelect.vm.$emit('update:modelValue', 'rerank')
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent(SwitchStub).props('disabled')).toBe(true)
    endpointSelect.vm.$emit('update:modelValue', 'auto')
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent(SwitchStub).props('disabled')).toBe(false)
    wrapper.unmount()
  })
})
