import { describe, expect, it } from 'vitest'

import router from './index'

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
