import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CredentialRemarkEditor from './CredentialRemarkEditor.vue'

const api = vi.hoisted(() => ({ put: vi.fn(), error: vi.fn(), success: vi.fn(), warning: vi.fn() }))
vi.mock('../api/client', () => ({ apiPut: api.put }))
vi.mock('../lib/error', () => ({ showError: api.error, showSuccess: api.success, showWarning: api.warning }))
const Button = defineComponent({ props: ['disabled', 'loading'], setup: (props, { slots }) => () => h('button', { disabled: props.disabled || props.loading }, slots.default?.()) })
const Input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { emit }) => () => h('textarea', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLTextAreaElement).value) }) })
function editor(remark = '原备注') {
  return mount(CredentialRemarkEditor, { props: { channelId: 3, credentialId: 9, remark }, global: { stubs: { ElButton: Button, ElInput: Input } } })
}
async function click(wrapper: ReturnType<typeof editor>, text: string) {
  const button = wrapper.findAll('button').find(item => item.text() === text)
  if (!button) throw new Error(`缺少按钮 ${text}`)
  await button.trigger('click')
}
function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>(done => { resolve = done })
  return { promise, resolve }
}
beforeEach(() => { vi.clearAllMocks(); api.put.mockResolvedValue(undefined) })

describe('凭据备注编辑', () => {
  it('展示原文，保存成功前不发出更新，成功后提交备注', async () => {
    const request = deferred()
    api.put.mockReturnValue(request.promise)
    const wrapper = editor()
    expect(wrapper.text()).toContain('原备注')
    await click(wrapper, '编辑备注')
    expect(wrapper.find('textarea').element.value).toBe('原备注')
    await wrapper.find('textarea').setValue('新备注\n第二行')
    await click(wrapper, '保存备注')
    expect(api.put).toHaveBeenCalledWith('/channels/3/credentials/9/remark', { remark: '新备注\n第二行' })
    expect(wrapper.emitted('saved')).toBeUndefined()
    request.resolve()
    await flushPromises()
    expect(wrapper.emitted('saved')).toEqual([['新备注\n第二行']])
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(api.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('清空后保存以空串清除，未保存清空不更新父级', async () => {
    const wrapper = editor()
    await click(wrapper, '编辑备注')
    await click(wrapper, '清空')
    expect(wrapper.emitted('saved')).toBeUndefined()
    await click(wrapper, '保存备注')
    await flushPromises()
    expect(api.put).toHaveBeenCalledWith('/channels/3/credentials/9/remark', { remark: '' })
    expect(wrapper.emitted('saved')).toEqual([['']])
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(api.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('保存失败保留草稿和原备注，不虚假更新并可重试', async () => {
    api.put.mockRejectedValueOnce(new Error('失败'))
    const wrapper = editor()
    await click(wrapper, '编辑备注')
    await wrapper.find('textarea').setValue('草稿')
    await click(wrapper, '保存备注')
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.props('remark')).toBe('原备注')
    expect(wrapper.find('textarea').element.value).toBe('草稿')
    expect(api.error).toHaveBeenCalled()
    await click(wrapper, '保存备注')
    await flushPromises()
    expect(wrapper.emitted('saved')).toEqual([['草稿']])
    wrapper.unmount()
  })

  it('平台登录生成的无备注密钥同样可以编辑，支持取消', async () => {
    const wrapper = editor('')
    await click(wrapper, '添加备注')
    await wrapper.find('textarea').setValue('不保存')
    await click(wrapper, '取消')
    expect(api.put).not.toHaveBeenCalled()
    await click(wrapper, '添加备注')
    expect(wrapper.find('textarea').element.value).toBe('')
    wrapper.unmount()
  })

  it('超过 500 字符不提交，恰好 500 字符可以保存', async () => {
    const wrapper = editor()
    await click(wrapper, '编辑备注')
    await wrapper.find('textarea').setValue('字'.repeat(501))
    await click(wrapper, '保存备注')
    expect(api.put).not.toHaveBeenCalled()
    expect(api.warning).toHaveBeenCalled()
    await wrapper.find('textarea').setValue('字'.repeat(500))
    await click(wrapper, '保存备注')
    await flushPromises()
    expect(api.put).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('切换渠道后忽略迟到保存，不更新新渠道', async () => {
    const request = deferred()
    api.put.mockReturnValue(request.promise)
    const wrapper = editor()
    await click(wrapper, '编辑备注')
    await wrapper.find('textarea').setValue('旧渠道')
    await click(wrapper, '保存备注')
    await wrapper.setProps({ channelId: 4, remark: '新渠道' })
    request.resolve()
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(api.success).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('新渠道')
    wrapper.unmount()
  })
})
