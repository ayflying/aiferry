import { ref, shallowRef } from 'vue'
import { apiGet, apiPost } from '../api/client'
import type { Channel, ChannelCredential, DiscoveredModel } from '../api/types'
import { geminiSubscriptionType } from '../lib/channelForm'

export function useModelDiscoverySource() {
  const accounts = shallowRef<ChannelCredential[]>([])
  const credentialId = ref<number>()
  const accountsError = ref('')
  let generation = 0

  function reset() {
    accounts.value = []
    credentialId.value = undefined
    accountsError.value = ''
    return nextRequest()
  }

  function nextRequest() { return ++generation }
  function isCurrent(request: number) { return request === generation }

  async function loadAccounts(channel: Channel, request: number) {
    if (channel.type !== geminiSubscriptionType) return
    accountsError.value = ''
    try {
      const result = await apiGet<ChannelCredential[]>(`/channels/${channel.id}/credentials`)
      if (isCurrent(request)) accounts.value = result.filter(row => row.status === 1 && !row.autoDisabled)
    } catch (error) {
      if (isCurrent(request)) accountsError.value = error instanceof Error ? error.message : '账号列表加载失败'
    }
  }

  function fetchModels(channel: Channel) {
    const path = `/channels/${channel.id}/models/discover`
    if (channel.type !== geminiSubscriptionType) return apiPost<DiscoveredModel[]>(path)
    return credentialId.value ? apiPost<DiscoveredModel[]>(path, { credentialId: credentialId.value }) : Promise.resolve([])
  }

  return { accounts, credentialId, accountsError, reset, nextRequest, isCurrent, loadAccounts, fetchModels }
}
