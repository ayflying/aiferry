import { flushPromises, shallowMount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel, ChannelType } from '../api/types'
import { useAppStore } from '../stores/app'
import ChannelCredentialDrawer from '../components/ChannelCredentialDrawer.vue'
import ChannelListPanel from '../components/ChannelListPanel.vue'
import ChannelsView from './ChannelsView.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), error: vi.fn() }))
vi.mock('../api/client', () => ({ apiGet: mocks.get, apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }))
vi.mock('../lib/error', () => ({ showError: mocks.error, showSuccess: vi.fn() }))
vi.mock('../stores/auth', () => ({ useAuthStore: () => ({ user: { isAdmin: true } }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ meta: { channelTab: 'channels' } }) }))

const channel = { id: 35, name: 'WorkBuddy', type: 'workbuddy' } as Channel
const loginType = {
  code: 'workbuddy',
  config: { models: {}, costs: {}, pricing: {}, login: { adapter: 'external_link' } },
} as ChannelType
const manualType = {
  code: 'openai', config: { models: {}, costs: {}, pricing: {} },
} as ChannelType

beforeEach(() => {
  setActivePinia(createPinia())
  mocks.get.mockReset().mockResolvedValue([])
  mocks.error.mockReset()
})

function mountView() {
  return shallowMount(ChannelsView)
}
function openCredentials(wrapper: ReturnType<typeof mountView>, value = channel) {
  wrapper.findComponent(ChannelListPanel).vm.$emit('open-credentials', value)
}

describe('管理员直接进入渠道列表的密钥入口', () => {
  it('首次进入无需访问渠道类型页，配置就绪后才打开登录抽屉', async () => {
    let resolve!: (value: ChannelType[]) => void
    mocks.get.mockImplementation((path: string) => path === '/channel-types'
      ? new Promise<ChannelType[]>(done => { resolve = done }) : Promise.resolve([]))
    const wrapper = mountView()
    await flushPromises()
    expect(mocks.get).not.toHaveBeenCalledWith('/channel-types')
    openCredentials(wrapper)
    await flushPromises()
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('modelValue')).toBe(false)
    resolve([loginType])
    await flushPromises()
    const drawer = wrapper.findComponent(ChannelCredentialDrawer)
    expect(drawer.props('modelValue')).toBe(true)
    expect(drawer.props('loginSupported')).toBe(true)
    expect(drawer.props('channel')).toMatchObject({ id: 35 })
    wrapper.unmount()
  })

  it('配置加载失败明确报错，不打开手动输入抽屉，并允许重试', async () => {
    const failure = new Error('网络不可用')
    mocks.get.mockImplementation((path: string) => path === '/channel-types'
      ? Promise.reject(failure) : Promise.resolve([]))
    const wrapper = mountView()
    await flushPromises()
    openCredentials(wrapper)
    await flushPromises()
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('modelValue')).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith(failure, '加载渠道密钥配置失败')
    mocks.get.mockResolvedValue([loginType])
    openCredentials(wrapper)
    await flushPromises()
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('loginSupported')).toBe(true)
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('modelValue')).toBe(true)
    wrapper.unmount()
  })

  it('类型配置缺失时拒绝打开，而不是误判不支持登录', async () => {
    const wrapper = mountView()
    await flushPromises()
    openCredentials(wrapper)
    await flushPromises()
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('modelValue')).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith(expect.objectContaining({
      message: '未找到该渠道的类型配置，请刷新渠道类型后重试',
    }), '加载渠道密钥配置失败')
    mocks.get.mockResolvedValue([loginType])
    openCredentials(wrapper)
    await flushPromises()
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('modelValue')).toBe(true)
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('loginSupported')).toBe(true)
    wrapper.unmount()
  })

  it('重复点击只发起一次能力加载', async () => {
    let resolve!: (value: ChannelType[]) => void
    mocks.get.mockImplementation((path: string) => path === '/channel-types'
      ? new Promise<ChannelType[]>(done => { resolve = done }) : Promise.resolve([]))
    const wrapper = mountView()
    await flushPromises()
    openCredentials(wrapper)
    openCredentials(wrapper)
    await flushPromises()
    expect(mocks.get.mock.calls.filter(([path]) => path === '/channel-types')).toHaveLength(1)
    resolve([loginType])
    await flushPromises()
    wrapper.unmount()
  })

  it('普通渠道仍走手动密钥，并复用已加载配置', async () => {
    mocks.get.mockImplementation((path: string) => Promise.resolve(path === '/channel-types' ? [manualType] : []))
    const wrapper = mountView()
    await flushPromises()
    openCredentials(wrapper, { ...channel, type: 'openai' })
    await flushPromises()
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('modelValue')).toBe(true)
    expect(wrapper.findComponent(ChannelCredentialDrawer).props('loginSupported')).toBe(false)
    openCredentials(wrapper, { ...channel, type: 'openai' })
    await flushPromises()
    expect(mocks.get.mock.calls.filter(([path]) => path === '/channel-types')).toHaveLength(1)
    expect(useAppStore().channelTypes).toHaveLength(1)
    wrapper.unmount()
  })
})
