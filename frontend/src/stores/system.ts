import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { loadAuthConfig } from '../api/auth'
import type { SystemInformationSettings } from '../api/types'

export const defaultSystemInformation: SystemInformationSettings = {
  systemName: 'AiFerry',
  serverUrl: '',
  logoUrl: '',
  footer: '',
  about: '',
  homeContent: '',
  userAgreement: '',
  privacyPolicy: '',
}

function normalize(value?: Partial<SystemInformationSettings>): SystemInformationSettings {
  return {
    ...defaultSystemInformation,
    ...value,
    systemName: value?.systemName?.trim() || defaultSystemInformation.systemName,
  }
}

export const useSystemStore = defineStore('system', () => {
  const information = ref<SystemInformationSettings>({ ...defaultSystemInformation })
  const loaded = ref(false)
  let pending: Promise<void> | undefined

  const systemName = computed(() => information.value.systemName)
  const logoUrl = computed(() => information.value.logoUrl || '/aiferry-logo.png')

  function apply(value?: Partial<SystemInformationSettings>) {
    information.value = normalize(value)
    loaded.value = true
  }

  async function load(force = false) {
    if (loaded.value && !force) return
    if (!pending || force) {
      pending = loadAuthConfig().then((config) => apply(config.system)).finally(() => { pending = undefined })
    }
    return pending
  }

  return { information, loaded, systemName, logoUrl, apply, load }
})
