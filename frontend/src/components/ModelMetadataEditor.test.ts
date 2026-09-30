import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { ModelMetadata } from '../api/types/model'
import ModelMetadataEditor from './ModelMetadataEditor.vue'

function metadata(): ModelMetadata {
  return { display_name: null, description: null, input_modalities: [], output_modalities: null, context_length: null, max_output_tokens: null, capabilities: { tools: false, reasoning: null, structured_output: true } }
}
function editor(manual: ModelMetadata | null = metadata()) {
  const automatic = metadata()
  return mount(ModelMetadataEditor, { props: { data: { automatic, manual, effective: automatic }, loading: false, saving: false } })
}
describe('模型能力编辑器', () => {
  it('保存保留 false、空列表和未知，不修改输入对象', async () => {
    const manual = metadata()
    const wrapper = editor(manual)
    const save = wrapper.findAll('button').find(button => button.text() === '保存能力')!
    await save.trigger('click')
    const result = wrapper.emitted('save')![0][0] as ModelMetadata
    expect(result.input_modalities).toEqual([])
    expect(result.output_modalities).toBeNull()
    expect(result.capabilities.tools).toBe(false)
    expect(result.capabilities.reasoning).toBeNull()
    expect(result).not.toBe(manual)
    expect(result.capabilities).not.toBe(manual.capabilities)
  })
  it('清除覆盖发送 null', async () => {
    const wrapper = editor()
    await wrapper.findAll('button').find(button => button.text() === '清除覆盖，恢复自动')!.trigger('click')
    expect(wrapper.emitted('save')).toEqual([[null]])
  })
  it('无覆盖时不允许重复清除', () => {
    const wrapper = editor(null)
    expect(wrapper.findAll('button').find(button => button.text() === '清除覆盖，恢复自动')!.attributes('disabled')).toBeDefined()
  })
  it('保存期间禁用重复提交', async () => {
    const wrapper = editor()
    await wrapper.setProps({ saving: true })
    await wrapper.findAll('button').find(button => button.text() === '保存能力')!.trigger('click')
    expect(wrapper.emitted('save')).toBeUndefined()
  })
})
