<script setup lang="ts">
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { onMounted, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { loadAuthConfig } from './api/auth'
import AppShell from './components/AppShell.vue'
import { setDisplayTimeZone } from './lib/format'
import { useSystemStore } from './stores/system'

const route = useRoute()
const system = useSystemStore()

onMounted(async () => {
  try {
    const config = await loadAuthConfig()
    setDisplayTimeZone(config.timeZone)
    system.apply(config.system)
  } catch {
    setDisplayTimeZone()
  }
})
watchEffect(() => {
  document.title = `${String(route.meta.title || '控制台')} - ${system.systemName}`
  let icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!icon) {
    icon = document.createElement('link')
    icon.rel = 'icon'
    document.head.append(icon)
  }
  icon.href = system.logoUrl
})
</script>

<template>
  <el-config-provider :locale="zhCn">
    <router-view v-if="route.meta.public" />
    <AppShell v-else />
  </el-config-provider>
</template>
