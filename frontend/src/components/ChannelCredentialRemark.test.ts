import { flushPromises, shallowMount } from '@vue/test-utils'
import { computed, defineComponent, h, inject, provide, type ComputedRef } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel, ChannelCredential } from '../api/types'
import ChannelCredentialDrawer from './ChannelCredentialDrawer.vue'
import CredentialRemarkEditor from './CredentialRemarkEditor.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), error: vi.fn() }))
vi.mock('../api/client', () => ({ apiGet: api.get, apiPost: api.post, apiPut: vi.fn(), apiDelete: vi.fn() }))
vi.mock('../lib/error', () => ({ showError: api.error, showSuccess: vi.fn(), showWarning: vi.fn() }))
const Slot = defineComponent({ setup: (_, { slots }) => () => h('div', [slots.default?.(), slots.footer?.()]) })
const Dialog = defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { 'data-title': props.title }, [slots.default?.(), slots.footer?.()]) : null })
const Button = defineComponent({ props: ['disabled', 'loading'], setup: (props, { slots }) => () => h('button', { disabled: props.disabled || props.loading }, slots.default?.()) })
const Table = defineComponent({ props: ['data'], setup: (props, { slots }) => { provide('rows', computed(() => props.data)); return () => h('div', slots.default?.()) } })
const Column = defineComponent({ setup: (_, { slots }) => { const rows = inject<ComputedRef<ChannelCredential[]>>('rows')!; return () => h('div', rows.value.map(row => slots.default?.({ row }))) } })
const channel = { id: 35, name: '测试渠道' } as Channel
const row = { id: 9, index: 1, keyPrefix: 'sk-test', remark: '账号备注', status: 1 } as ChannelCredential
function drawer() {
  return shallowMount(ChannelCredentialDrawer, { props: { modelValue: false, channel }, global: { stubs: { ElDrawer: Slot, ElDialog: Dialog, ElButton: Button, ElTable: Table, ElTableColumn: Column } } })
}
async function open(wrapper: ReturnType<typeof drawer>) {
  await wrapper.setProps({ modelValue: true })
  await flushPromises()
}
async function add(wrapper: ReturnType<typeof drawer>, remark: string) {
  await wrapper.findAll('button').find(item => item.text() === '添加密钥')!.trigger('click')
  const dialog = wrapper.find('[data-title="添加上游密钥"]')
  const inputs = dialog.findAllComponents({ name: 'ElInput' })
  inputs[0]!.vm.$emit('update:modelValue', ' key ')
  inputs[inputs.length - 1]!.vm.$emit('update:modelValue', remark)
  await flushPromises()
  await dialog.findAll('button')[1]!.trigger('click')
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
beforeEach(() => { vi.clearAllMocks(); api.get.mockResolvedValue([row]); api.post.mockResolvedValue(undefined) })

describe('渠道密钥备注编排', () => {
  it('添加 payload 包含备注及原有管理密钥，成功后刷新', async () => {
    const wrapper = drawer()
    await wrapper.setProps({ managementKeySupported: true })
    await open(wrapper)
    await wrapper.findAll('button').find(item => item.text() === '添加密钥')!.trigger('click')
    const dialog = wrapper.find('[data-title="添加上游密钥"]')
    const inputs = dialog.findAllComponents({ name: 'ElInput' })
    inputs[0]!.vm.$emit('update:modelValue', ' key ')
    inputs[1]!.vm.$emit('update:modelValue', ' mgmt ')
    inputs[2]!.vm.$emit('update:modelValue', ' 工作账号 ')
    await flushPromises()
    await dialog.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/channels/35/credentials', { apiKey: 'key', managementKey: 'mgmt', remark: ' 工作账号 ' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.findComponent(CredentialRemarkEditor).props('remark')).toBe('账号备注')
    wrapper.unmount()
  })

  it('未填写备注保持可选 payload', async () => {
    const wrapper = drawer()
    await open(wrapper)
    await add(wrapper, '')
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/channels/35/credentials', { apiKey: 'key' })
    wrapper.unmount()
  })

  it('添加失败保留输入和弹窗，不发出 changed', async () => {
    api.post.mockRejectedValue(new Error('保存失败'))
    const wrapper = drawer()
    await open(wrapper)
    await add(wrapper, '保留草稿')
    await flushPromises()
    expect(wrapper.find('[data-title="添加上游密钥"]').exists()).toBe(true)
    const inputs = wrapper.find('[data-title="添加上游密钥"]').findAllComponents({ name: 'ElInput' })
    expect(inputs[inputs.length - 1]!.attributes('modelvalue')).toBe('保留草稿')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(api.error).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('渠道切换清除旧行并忽略迟到列表', async () => {
    const request = deferred<ChannelCredential[]>()
    api.get.mockReturnValueOnce(request.promise).mockResolvedValueOnce([{ ...row, id: 10, remark: '新渠道备注' }])
    const wrapper = drawer()
    await wrapper.setProps({ modelValue: true })
    await wrapper.setProps({ channel: { ...channel, id: 36 } })
    await flushPromises()
    request.resolve([row])
    await flushPromises()
    expect(wrapper.findComponent(CredentialRemarkEditor).props()).toMatchObject({ channelId: 36, credentialId: 10, remark: '新渠道备注' })
    wrapper.unmount()
  })

  it('渠道切换忽略迟到添加，不关闭新表单或刷新新渠道', async () => {
    const request = deferred<void>()
    api.post.mockReturnValue(request.promise)
    const wrapper = drawer()
    await open(wrapper)
    await add(wrapper, '旧渠道备注')
    await wrapper.setProps({ channel: { ...channel, id: 36 } })
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === '添加密钥')!.trigger('click')
    const count = api.get.mock.calls.length
    request.resolve()
    await flushPromises()
    expect(wrapper.find('[data-title="添加上游密钥"]').exists()).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(count)
    expect(wrapper.emitted('changed')).toBeUndefined()
    wrapper.unmount()
  })
})
