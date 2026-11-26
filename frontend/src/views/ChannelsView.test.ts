import { flushPromises, shallowMount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel, ChannelType } from '../api/types'
import { useAppStore } from '../stores/app'
import ChannelAdvancedSettings from '../components/ChannelAdvancedSettings.vue'
import ChannelCredentialDrawer from '../components/ChannelCredentialDrawer.vue'
import ChannelRouteCoverageSettings from '../components/ChannelRouteCoverageSettings.vue'
import ChannelListPanel from '../components/ChannelListPanel.vue'
import ChannelModelMappingDialog from '../components/ChannelModelMappingDialog.vue'
import ChannelsView from './ChannelsView.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), error: vi.fn() }))
vi.mock('../api/client', () => ({ apiGet: mocks.get, apiPost: mocks.post, apiPut: mocks.put, apiDelete: vi.fn() }))
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
  mocks.post.mockReset().mockResolvedValue({})
  mocks.put.mockReset().mockResolvedValue({})
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

  it('渠道类型下拉优先向下，仅允许在上下方向避让，不侧向展开', async () => {
    const wrapper = shallowMount(ChannelsView, { global: { renderStubDefaultSlot: true } })
    await flushPromises()
    wrapper.findComponent(ChannelListPanel).vm.$emit('create')
    await flushPromises()
    const select = wrapper.findComponent({ name: 'ElSelect' })
    expect(select.attributes('placeholder')).toBe('选择渠道类型')
    expect(select.attributes('placement')).toBe('bottom-start')
    expect(select.props('fallbackPlacements')).toEqual(['bottom-start', 'top-start'])
    wrapper.unmount()
  })

  it.each(['account-access-token', ''])('Codex 创建可选账号 token=%j，填写保留、留空也能保存', async (token) => {
    mocks.get.mockImplementation((path: string) => Promise.resolve(path === '/channel-types' ? [{ code: 'codex', name: 'Codex', status: 1, config: { baseUrl: 'https://chatgpt.com/backend-api/codex', login: { adapter: 'codex_device' } } }] : []))
    const wrapper = shallowMount(ChannelsView, { global: { renderStubDefaultSlot: true, stubs: { ElDrawer: { template: '<section><slot /><slot name="footer" /></section>' } } } })
    await flushPromises()
    wrapper.findComponent(ChannelListPanel).vm.$emit('create')
    await flushPromises()
    expect(wrapper.text()).toContain('可留空，保存渠道后')
    expect(wrapper.text()).toContain('过期后需重新填写')
    expect(wrapper.findAllComponents({ name: 'ElFormItem' }).some(item => item.attributes('label') === '首个推理密钥（可选）')).toBe(true)
    const inputs = wrapper.findAllComponents({ name: 'ElInput' })
    const tokenInput = inputs.find(item => item.attributes('placeholder')?.includes('账号 access token（非 sk API Key）'))!
    expect(tokenInput.exists()).toBe(true)
    inputs.find(item => item.attributes('placeholder') === '例如 OpenAI 主线路')!.vm.$emit('update:modelValue', 'Codex fixture')
    tokenInput.vm.$emit('update:modelValue', token)
    await flushPromises()
    const save = wrapper.findAllComponents({ name: 'ElButton' }).find(item => item.text() === '保存渠道')!
    save.vm.$emit('click')
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledWith('/channels', expect.objectContaining({ name: 'Codex fixture', type: 'codex', apiKey: token }))
    wrapper.unmount()
  })

  it('Codex 设备登录能力仍传入抽屉，保留平台登录入口', async () => {
    mocks.get.mockImplementation((path: string) => Promise.resolve(path === '/channel-types' ? [{ code: 'codex', config: { models: {}, costs: {}, pricing: {}, login: { adapter: 'codex_device' } } }] : []))
    const wrapper = mountView()
    await flushPromises()
    openCredentials(wrapper, { ...channel, type: 'codex' })
    await flushPromises()
    const drawer = wrapper.findComponent(ChannelCredentialDrawer)
    expect(drawer.props('loginSupported')).toBe(true)
    expect(drawer.props('loginAdapter')).toBe('codex_device')
    wrapper.unmount()
  })

  const geminiBaseUrl = 'https://generativelanguage.googleapis.com/v1beta/openai'
  const geminiType = {
    code: 'gemini', name: 'Google Gemini', status: 1,
    config: { baseUrl: geminiBaseUrl, models: {}, costs: {}, pricing: {} },
  } as ChannelType
  const geminiChannel = {
    ...channel, type: geminiType.code, name: 'Google Gemini', baseUrl: geminiBaseUrl, hasProxy: true,
    priority: 0, weight: 1, healthCheckModelId: 0, autoDisableEnabled: true,
  }
  function mountForm() {
    return shallowMount(ChannelsView, { global: { renderStubDefaultSlot: true, stubs: { ElDrawer: { template: '<section><slot /><slot name="footer" /></section>' } } } })
  }

  it('普通 Gemini API 使用类型预设地址、手动密钥和可选代理', async () => {
    mocks.get.mockImplementation((path: string) => Promise.resolve(path === '/channel-types' ? [geminiType] : []))
    const wrapper = mountForm()
    wrapper.findComponent(ChannelListPanel).vm.$emit('create')
    await flushPromises()
    const inputs = wrapper.findAllComponents({ name: 'ElInput' })
    const baseUrl = inputs.find(input => input.attributes('placeholder') === geminiBaseUrl)!
    expect(baseUrl.props('modelValue')).toBe(geminiBaseUrl)
    const customUrl = 'https://gemini.example.test/v1'
    baseUrl.vm.$emit('update:modelValue', customUrl)
    inputs.find(input => input.attributes('placeholder') === '例如 OpenAI 主线路')!.vm.$emit('update:modelValue', 'Gemini fixture')
    inputs.find(input => input.attributes('placeholder')?.includes('sk-...'))!.vm.$emit('update:modelValue', 'fixture-key')
    const backupBaseUrls = ['https://cdn.example.test/v1']
    wrapper.findComponent(ChannelRouteCoverageSettings).vm.$emit('update:backupBaseUrls', backupBaseUrls)
    wrapper.findAllComponents({ name: 'ElButton' }).find(item => item.text() === '保存渠道')!.vm.$emit('click')
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledWith('/channels', expect.objectContaining({
      type: geminiType.code, baseUrl: customUrl, apiKey: 'fixture-key', proxyUrl: '',
      advancedConfig: expect.objectContaining({ backupBaseUrls }),
    }))
    openCredentials(wrapper, geminiChannel)
    await flushPromises()
    const drawer = wrapper.findComponent(ChannelCredentialDrawer)
    expect(drawer.props('modelValue')).toBe(true)
    expect(drawer.props('loginSupported')).toBe(false)
    wrapper.unmount()
  })

  it('编辑普通 Gemini 渠道时保留未能读取的代理，但允许明确清空', async () => {
    mocks.get.mockImplementation((path: string) => {
      if (path.endsWith('/proxy')) return Promise.reject(new Error('代理读取失败'))
      return Promise.resolve(path === '/channel-types' ? [geminiType] : [])
    })
    const wrapper = mountForm()
    wrapper.findComponent(ChannelListPanel).vm.$emit('edit', geminiChannel)
    await flushPromises()
    const save = wrapper.findAllComponents({ name: 'ElButton' }).find(item => item.text() === '保存渠道')!
    save.vm.$emit('click')
    await flushPromises()
    const [path, payload] = mocks.put.mock.lastCall!
    expect(path).toBe(`/channels/${geminiChannel.id}`)
    expect(payload).not.toHaveProperty('proxyUrl')
    wrapper.findComponent(ChannelListPanel).vm.$emit('edit', geminiChannel)
    await flushPromises()
    wrapper.findComponent(ChannelAdvancedSettings).vm.$emit('update:proxyUrl', '')
    save.vm.$emit('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenLastCalledWith(path, expect.objectContaining({ proxyUrl: '' }))
    wrapper.unmount()
  })

  it('普通 Gemini 直接发现模型，失败重试并保留已保存的渠道模型', async () => {
    mocks.get.mockImplementation((path: string) => Promise.resolve(path.endsWith('/models')
      ? [{ id: 5, publicName: 'saved', upstreamName: 'gemini-old', enabled: 1 }] : []))
    mocks.post.mockRejectedValueOnce(new Error('模型接口失败')).mockResolvedValueOnce([{ name: 'gemini-new', selected: false }])
    const wrapper = mountView()
    wrapper.findComponent(ChannelListPanel).vm.$emit('discover', geminiChannel)
    await flushPromises()
    const dialog = wrapper.findComponent(ChannelModelMappingDialog)
    expect(mocks.get).not.toHaveBeenCalledWith('/channels/35/credentials')
    expect(mocks.post).toHaveBeenCalledExactlyOnceWith('/channels/35/models/discover')
    expect(dialog.props('discoveryError')).toContain('模型接口失败')
    expect(dialog.props('selectedModelNames')).toEqual(['gemini-old'])
    dialog.vm.$emit('retry')
    await flushPromises()
    expect(mocks.post).toHaveBeenLastCalledWith('/channels/35/models/discover')
    expect(dialog.props('discoveryError')).toBe('')
    expect(dialog.props('discoveredModels').map(model => model.name)).toEqual(['gemini-old', 'gemini-new'])
    expect(dialog.props('selectedModelNames')).toEqual(['gemini-old'])
    expect(dialog.props('modelMappings')).toEqual([{ id: 5, upstreamName: 'gemini-old', publicName: 'saved' }])
    dialog.vm.$emit('update:selectedModelNames', ['gemini-old', 'gemini-new'])
    dialog.vm.$emit('save')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/channels/35/models/selection', { models: [
      { upstreamName: 'gemini-old', publicName: 'saved' },
      { upstreamName: 'gemini-new', publicName: 'gemini-new' },
    ] })
    wrapper.unmount()
  })

  it('已保存模型加载失败时，不允许覆盖渠道模型配置', async () => {
    mocks.post.mockResolvedValue([])
    mocks.get.mockImplementation((path: string) => path.endsWith('/models')
      ? Promise.reject(new Error('暂时不可用')) : Promise.resolve([]))
    const wrapper = mountView()
    wrapper.findComponent(ChannelListPanel).vm.$emit('discover', geminiChannel)
    await flushPromises()
    const dialog = wrapper.findComponent(ChannelModelMappingDialog)
    expect(dialog.props('savedModelsError')).toBe('暂时不可用')
    dialog.vm.$emit('save')
    expect(mocks.put).not.toHaveBeenCalled()
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
