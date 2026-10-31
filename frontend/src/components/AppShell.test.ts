import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AppShell from './AppShell.vue'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/usage', meta: { title: '用量日志' } }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))
vi.mock('../stores/auth', () => ({ useAuthStore: () => ({ user: { name: '测试用户', isAdmin: true } }) }))
vi.mock('../stores/system', () => ({ useSystemStore: () => ({ systemName: 'AiFerry', logoUrl: '/logo.png' }) }))
vi.mock('./SiteFooter.vue', () => ({ default: { template: '<footer />' } }))

function mountShell(width = 1280) {
  vi.stubGlobal('innerWidth', width)
  return mount(AppShell, {
    global: { stubs: {
      ElTooltip: { inheritAttrs: false, template: '<slot />' },
      ElDrawer: true,
      ElDropdown: { inheritAttrs: false, emits: ['command'], template: '<slot />' },
      ElAvatar: true,
      RouterView: true,
    } },
  })
}

afterEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe('侧栏底部折叠入口', () => {
  it('收起、展开与底部入口共用状态，并记忆选择', async () => {
    const wrapper = mountShell()
    const toggle = wrapper.get('.sidebar-toggle[aria-label="收起侧栏"]')
    await toggle.trigger('click')
    expect(wrapper.get('.sidebar').classes()).toContain('collapsed')
    expect(localStorage.getItem('aiferry-sidebar')).toBe('collapsed')
    expect(wrapper.get('.sidebar-toggle[aria-label="展开侧栏"]').attributes('aria-expanded')).toBe('false')
    await wrapper.get('.sidebar-toggle').trigger('click')
    expect(wrapper.get('.sidebar').classes()).not.toContain('collapsed')
    expect(localStorage.getItem('aiferry-sidebar')).toBe('expanded')
    wrapper.unmount()
  })

  it('重新挂载保留折叠状态', () => {
    localStorage.setItem('aiferry-sidebar', 'collapsed')
    const wrapper = mountShell()
    expect(wrapper.get('.sidebar').classes()).toContain('collapsed')
    expect(wrapper.find('.sidebar-toggle[aria-label="展开侧栏"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('移动端仍使用抽屉入口，不展示桌面折叠按钮', async () => {
    const wrapper = mountShell(390)
    await nextTick()
    expect(wrapper.find('.sidebar').exists()).toBe(false)
    expect(wrapper.find('button[aria-label="打开导航"]').exists()).toBe(true)
    expect(wrapper.find('button[aria-label="收起菜单"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
