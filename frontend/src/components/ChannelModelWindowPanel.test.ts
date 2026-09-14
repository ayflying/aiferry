import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'

import type { TimeWindow } from '../api/types'
import ChannelModelWindowPanel from './ChannelModelWindowPanel.vue'
import TimeWindowEditor from './TimeWindowEditor.vue'

const models = [
  { publicName: 'gpt-x', upstreamName: 'gpt-x' },
  { publicName: 'gpt-y', upstreamName: 'gpt-y' },
]

/**
 * 复刻 ChannelsView 的用法：父级持有记录，收到 update:windows 后**原样存回**
 * （对象身份保持，供面板识别自己的回声），并在切换渠道时先清空再回填。
 */
function mountPanel() {
  const windows = ref<Record<string, TimeWindow>>({})
  const Host = defineComponent({
    setup() {
      return () => h(ChannelModelWindowPanel, {
        models,
        windows: windows.value,
        'onUpdate:windows': (value: Record<string, TimeWindow>) => { windows.value = value },
      })
    },
  })
  // el-select 的交互由组件事件驱动，这里桩掉避免引入 Element Plus 的下拉渲染依赖。
  const wrapper = mount(Host, { global: { stubs: { ElSelect: true } } })
  return { wrapper, windows }
}

function addButton(wrapper: ReturnType<typeof mountPanel>['wrapper']) {
  const button = wrapper.findAll('button').find((item) => item.text().includes('添加关闭时段'))
  if (!button) throw new Error('未找到「添加关闭时段」按钮')
  return button
}

function selectModel(wrapper: ReturnType<typeof mountPanel>['wrapper'], value: string) {
  wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', value)
}

describe('ChannelModelWindowPanel', () => {
  it('父级清空再回填时按外部数据重建行，哪怕内容与上次提交完全相同', async () => {
    const { wrapper, windows } = mountPanel()

    await addButton(wrapper).trigger('click')
    selectModel(wrapper, 'gpt-x')
    await nextTick()

    expect(windows.value['gpt-x']).toBeTruthy()
    expect(wrapper.text()).toContain('已配置 1 条关闭时段')

    // 保存后重新打开弹窗：ChannelsView.discover 先置空、再把库里的值回填回来。
    const saved = JSON.parse(JSON.stringify(windows.value)) as Record<string, TimeWindow>
    windows.value = {}
    await nextTick()
    windows.value = saved
    await nextTick()

    // 回填的内容与面板上次提交的一致，但它来自库、属于外部数据，必须照常重建行。
    expect(wrapper.text()).toContain('已配置 1 条关闭时段')
    expect(wrapper.text()).not.toContain('暂无定时关闭配置')
  })

  it('回灌自己刚发出的负载时不重建行，避免打断正在编辑的行', async () => {
    const { wrapper, windows } = mountPanel()

    // 新增的空行会随提交发出空负载（未选模型的行不进负载），回灌不得把它抹掉。
    await addButton(wrapper).trigger('click')
    await nextTick()

    expect(windows.value).toEqual({})
    expect(wrapper.text()).not.toContain('暂无定时关闭配置')

    selectModel(wrapper, 'gpt-x')
    await nextTick()
    expect(wrapper.text()).toContain('已配置 1 条关闭时段')
  })

  it('编辑时段后父级回灌不得收起展开的编辑器', async () => {
    const { wrapper, windows } = mountPanel()

    // 新增的行默认展开时段编辑器，选中模型后仍是展开状态。
    await addButton(wrapper).trigger('click')
    selectModel(wrapper, 'gpt-x')
    await nextTick()
    expect(wrapper.findComponent(TimeWindowEditor).exists()).toBe(true)

    // 编辑器每次修改都回抛一个新的时间窗，面板据此提交、父级随即回灌。
    wrapper.findComponent(TimeWindowEditor).vm.$emit('update:modelValue', { tz: 'UTC', weekdays: [1], ranges: [['08:00', '09:00']] })
    await nextTick()
    await nextTick()

    expect(windows.value['gpt-x']).toEqual({ tz: 'UTC', weekdays: [1], ranges: [['08:00', '09:00']] })
    expect(wrapper.findComponent(TimeWindowEditor).exists()).toBe(true)
    expect(wrapper.text()).toContain('已配置 1 条关闭时段')
  })
})
