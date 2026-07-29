import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { ensureUser } = vi.hoisted(() => ({ ensureUser: vi.fn().mockResolvedValue(false) }))

vi.mock('../stores/auth', () => ({
  useAuthStore: () => ({ ensureUser }),
}))

vi.mock('../views/LoginView.vue', () => ({ default: { name: 'LoginView' } }))
vi.mock('../views/HomeView.vue', () => ({ default: { name: 'HomeView' } }))
vi.mock('../views/HomepageUnavailableView.vue', () => ({ default: { name: 'HomepageUnavailableView' } }))

import router from './index'
import { useSystemStore } from '../stores/system'

describe('settings routes', () => {
  it.each([
    ['/settings', 'overview'],
    ['/settings/basic', 'basic'],
    ['/settings/resilience', 'resilience'],
    ['/settings/security', 'security'],
    ['/settings/mail', 'mail'],
  ])('maps %s to the %s settings section', (path, settingsTab) => {
    const route = router.resolve(path)

    expect(route.meta.admin).toBe(true)
    expect(route.meta.settingsTab).toBe(settingsTab)
  })
})

describe('public homepage route', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    ensureUser.mockReset()
    ensureUser.mockResolvedValue(false)
    await router.replace('/login')
  })

  it('shows an unavailable page instead of redirecting to login when the public homepage is disabled', async () => {
    useSystemStore().apply({ publicHomepageEnabled: false })

    await router.push('/')

    expect(router.currentRoute.value.name).toBe('homepage-unavailable')
  })

  it('does not redirect to login when the homepage configuration cannot be loaded', async () => {
    vi.spyOn(useSystemStore(), 'load').mockRejectedValueOnce(new Error('network unavailable'))

    await router.push('/')

    expect(router.currentRoute.value.name).toBe('homepage-unavailable')
  })

  it('allows the home route when the public homepage is enabled', async () => {
    useSystemStore().apply({ publicHomepageEnabled: true })

    await router.push('/')

    expect(router.currentRoute.value.name).toBe('home')
  })
})
