import { mount } from '@vue/test-utils'
import { h, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ResponsiveList from './ResponsiveList.vue'

let listener: (() => void) | undefined

function mockMatchMedia(matches: boolean) {
  const query = {
    matches,
    addEventListener: vi.fn((_: string, callback: () => void) => { listener = callback }),
    removeEventListener: vi.fn(),
  }
  vi.stubGlobal('matchMedia', vi.fn(() => query))
  return query
}

function mountList() {
  return mount(ResponsiveList, {
    slots: {
      desktop: () => h('div', { 'data-test': 'desktop' }, 'desktop'),
      mobile: () => h('div', { 'data-test': 'mobile' }, 'mobile'),
    },
  })
}

afterEach(() => {
  listener = undefined
  vi.unstubAllGlobals()
})

describe('ResponsiveList', () => {
  it('renders the matching layout and switches when the viewport changes', async () => {
    const query = mockMatchMedia(false)
    const wrapper = mountList()
    await nextTick()

    expect(wrapper.find('[data-test="desktop"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="mobile"]').exists()).toBe(false)

    query.matches = true
    listener?.()
    await nextTick()

    expect(wrapper.find('[data-test="desktop"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="mobile"]').exists()).toBe(true)
  })

  it('removes its media-query listener when unmounted', async () => {
    const query = mockMatchMedia(true)
    const wrapper = mountList()
    await nextTick()

    wrapper.unmount()

    expect(query.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })
})
