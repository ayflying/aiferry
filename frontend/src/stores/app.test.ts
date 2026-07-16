import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { apiGet } from '../api/client'
import { useAppStore } from './app'

vi.mock('../api/client', () => ({ apiGet: vi.fn() }))

describe('app store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(apiGet).mockReset()
  })

  it('normalizes a null API key response to an empty list', async () => {
    vi.mocked(apiGet).mockResolvedValueOnce(null)
    const store = useAppStore()

    await store.loadAPIKeys()

    expect(store.apiKeys).toEqual([])
  })

  it('normalizes a null channel type response to an empty list', async () => {
    vi.mocked(apiGet).mockResolvedValueOnce(null)
    const store = useAppStore()

    await store.loadChannelTypes()

    expect(store.channelTypes).toEqual([])
  })
})
