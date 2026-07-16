import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet } from '../api/client'
import type { APIKey, Channel, ChannelGroup, ChannelType } from '../api/types'

export const useAppStore = defineStore('app', () => {
  const channels = ref<Channel[]>([])
  const channelTypes = ref<ChannelType[]>([])
  const channelGroups = ref<ChannelGroup[]>([])
  const apiKeys = ref<APIKey[]>([])

  async function loadChannels() {
    channels.value = (await apiGet<Channel[] | null>('/channels')) ?? []
  }

  async function loadAPIKeys() {
    apiKeys.value = (await apiGet<APIKey[] | null>('/api-keys')) ?? []
  }

  async function loadChannelTypes() {
    channelTypes.value = (await apiGet<ChannelType[] | null>('/channel-types')) ?? []
  }

  async function loadChannelGroups() {
    channelGroups.value = (await apiGet<ChannelGroup[] | null>('/channel-groups')) ?? []
  }

  return { channels, channelTypes, channelGroups, apiKeys, loadChannels, loadChannelTypes, loadChannelGroups, loadAPIKeys }
})
