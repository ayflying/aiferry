import { mount } from '@vue/test-utils'
import { ElInputTag } from 'element-plus'
import { defineComponent, h, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import ChannelModelMappingDialog from './ChannelModelMappingDialog.vue'

function mountDialog() {
  const mappings = ref([{ id: 1, upstreamName: 'upstream', publicName: 'alpha,free' }])
  const Host = defineComponent({
    setup: () => () => h(ChannelModelMappingDialog, {
      modelValue: true, channelName: '测试渠道', discovering: false, discoveryError: '',
      applying: false, discoveredModels: [], selectedModelNames: ['upstream'],
      discoveryKeyword: '', modelMappings: mappings.value, windowModels: [], closedWindows: {},
      'onUpdate:modelMappings': (value) => { mappings.value = value },
    }),
  })
  const passthrough = { template: '<div><slot /></div>' }
  const wrapper = mount(Host, {
    global: {
      components: { ElInputTag },
      directives: { loading: () => {} },
      stubs: {
        ElDialog: passthrough, ElTabs: passthrough, ElTabPane: passthrough,
        ElButton: passthrough, ElTooltip: passthrough, ElSelect: true, ElInput: true,
        ChannelModelWindowPanel: true,
      },
    },
  })
  return { wrapper, mappings }
}

describe('映射公开名称标签输入', () => {
  it('回填独立标签，关闭单个标签不影响同一上游的其他名称', async () => {
    const { wrapper, mappings } = mountDialog()
    expect(wrapper.findComponent(ElInputTag).props('modelValue')).toEqual(['alpha', 'free'])
    await wrapper.find('.el-tag__close').trigger('click')
    expect(mappings.value).toEqual([{ id: 1, upstreamName: 'upstream', publicName: 'free' }])
    wrapper.unmount()
  })

  it('回车和逗号批量添加、去重，失焦提交未确认文本', async () => {
    const { wrapper, mappings } = mountDialog()
    const input = wrapper.find('.el-input-tag input')
    await input.setValue('next')
    await input.trigger('keydown', { key: 'Enter', code: 'Enter' })
    expect(mappings.value[0]!.publicName).toBe('alpha,free,next')
    await input.setValue('batch，other,alpha,')
    expect(mappings.value[0]!.publicName).toBe('alpha,free,next,batch,other')
    await input.setValue('last')
    await input.trigger('focus')
    await input.trigger('blur')
    expect(mappings.value[0]!.publicName).toBe('alpha,free,next,batch,other,last')
    wrapper.unmount()
  })
})
