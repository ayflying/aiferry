import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel } from '../api/types'
import ChannelCredentialDrawer from './ChannelCredentialDrawer.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), success: vi.fn() }))
vi.mock('../api/client', () => ({ apiGet: api.get, apiPost: api.post, apiPut: vi.fn(), apiDelete: vi.fn() }))
vi.mock('../lib/error', () => ({ showError: vi.fn(), showSuccess: api.success, showWarning: vi.fn() }))
const Slot = defineComponent({ setup: (_, { slots }) => () => h('div', [slots.default?.(), slots.footer?.()]) })
const RadioGroup = defineComponent({ name: 'ElRadioGroup', setup: (_, { slots }) => () => h('div', slots.default?.()) })
const Dialog = defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { 'data-title': props.title }, [slots.default?.(), slots.footer?.()]) : null })
const Button = defineComponent({ props: ['disabled', 'loading'], setup: (props, { slots }) => () => h('button', { disabled: props.disabled || props.loading }, slots.default?.()) })
const channel = { id: 35, name: 'WorkBuddy' } as Channel
function mountDrawer(loginSupported = true) {
  return shallowMount(ChannelCredentialDrawer, { props: { modelValue: true, channel, loginSupported }, global: { stubs: { ElDrawer: Slot, ElDialog: Dialog, ElButton: Button, ElRadioGroup: RadioGroup } } })
}
function button(wrapper: ReturnType<typeof mountDrawer>, label: string) {
  const result = wrapper.findAll('button').find(item => item.text() === label)
  if (!result) throw new Error(`未找到按钮：${label}`)
  return result
}

beforeEach(() => { vi.useFakeTimers(); api.get.mockResolvedValue([]); api.post.mockReset(); api.success.mockReset() })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('添加上游密钥', () => {
  it('支持登录的渠道默认展示平台登录，但打开添加弹窗不会自动发起请求', async () => {
    const wrapper = mountDrawer()
    await button(wrapper, '添加密钥').trigger('click')
    expect(wrapper.find('[data-title="添加上游密钥"]').exists()).toBe(true)
    expect(button(wrapper, '打开平台登录页').exists()).toBe(true)
    expect(api.post).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('不支持登录的渠道仅手动添加，并保留管理密钥', async () => {
    const wrapper = mountDrawer(false)
    await wrapper.setProps({ managementKeySupported: true })
    await button(wrapper, '添加密钥').trigger('click')
    expect(wrapper.text()).not.toContain('打开平台登录页')
    const inputs = wrapper.findAllComponents({ name: 'ElInput' })
    inputs[0]!.vm.$emit('update:modelValue', ' key ')
    inputs[1]!.vm.$emit('update:modelValue', ' management ')
    await flushPromises()
    await wrapper.find('[data-title="添加上游密钥"]').findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/channels/35/credentials', { apiKey: 'key', managementKey: 'management' })
    expect(wrapper.find('[data-title="添加上游密钥"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('用户点击时同步预开窗口，完成登录后刷新凭据', async () => {
    const replace = vi.fn()
    const open = vi.spyOn(window, 'open').mockReturnValue({ opener: {}, closed: false, location: { replace }, close: vi.fn() } as unknown as Window)
    api.post.mockResolvedValueOnce({ state: 'state', authUrl: 'https://www.workbuddy.cn/login' }).mockResolvedValueOnce({ status: 'completed' })
    const wrapper = mountDrawer()
    await button(wrapper, '添加密钥').trigger('click')
    await button(wrapper, '打开平台登录页').trigger('click')
    expect(open).toHaveBeenCalledWith('about:blank', '_blank')
    await flushPromises()
    expect(replace).toHaveBeenCalledWith('https://www.workbuddy.cn/login')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/channels/35/credentials/login/poll', { state: 'state' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.find('[data-title="登录上游账号"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('支持登录的渠道仍可切换为手动填写', async () => {
    const wrapper = mountDrawer()
    await button(wrapper, '添加密钥').trigger('click')
    wrapper.findComponent({ name: 'ElRadioGroup' }).vm.$emit('update:modelValue', 'manual')
    await flushPromises()
    expect(wrapper.find('[data-title="添加上游密钥"]').findComponent({ name: 'ElInput' }).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('打开平台登录页')
    expect(api.post).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('慢轮询不重叠，关闭后忽略迟到的完成响应', async () => {
    let resolve!: (value: unknown) => void
    api.post.mockResolvedValueOnce({ state: 'state', authUrl: 'https://www.workbuddy.cn/login' })
      .mockImplementationOnce(() => new Promise(done => { resolve = done }))
    vi.spyOn(window, 'open').mockReturnValue(null)
    const wrapper = mountDrawer()
    await button(wrapper, '添加密钥').trigger('click')
    await button(wrapper, '打开平台登录页').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    expect(api.post).toHaveBeenCalledTimes(2)
    await button(wrapper, '关闭').trigger('click')
    resolve({ status: 'completed' })
    await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(api.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('请求未返回时关闭抽屉，不会重新打开登录弹窗', async () => {
    let resolve!: (value: unknown) => void
    api.post.mockReturnValue(new Promise(done => { resolve = done }))
    const close = vi.fn()
    vi.spyOn(window, 'open').mockReturnValue({ opener: {}, close } as unknown as Window)
    const wrapper = mountDrawer()
    await button(wrapper, '添加密钥').trigger('click')
    await button(wrapper, '打开平台登录页').trigger('click')
    await wrapper.setProps({ modelValue: false })
    resolve({ state: 'stale', authUrl: 'https://www.workbuddy.cn/login' })
    await flushPromises()
    expect(close).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-title="登录上游账号"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(4000)
    expect(api.post).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
