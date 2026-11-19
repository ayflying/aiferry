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
  publicHomepageEnabled: false,
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
  // 运行时版本由服务端随公开配置下发，用于页脚展示「线上到底跑的是哪个版本」。
  const version = computed(() => information.value.version?.trim() || '')
  const revision = computed(() => information.value.revision?.trim() || '')

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

  return { information, loaded, systemName, logoUrl, version, revision, apply, load }
})
