import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet } from '../api/client'
import type { APIKey, Channel } from '../api/types'

export const useAppStore = defineStore('app', () => {
  const channels = ref<Channel[]>([])
  const apiKeys = ref<APIKey[]>([])

  async function loadChannels() {
    channels.value = (await apiGet<Channel[] | null>('/channels')) ?? []
  }

  async function loadAPIKeys() {
    apiKeys.value = (await apiGet<APIKey[] | null>('/api-keys')) ?? []
  }

  return { channels, apiKeys, loadChannels, loadAPIKeys }
})
