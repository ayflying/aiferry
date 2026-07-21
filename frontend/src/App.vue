<script setup lang="ts">
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { loadAuthConfig } from './api/auth'
import AppShell from './components/AppShell.vue'
import { setDisplayTimeZone } from './lib/format'

const route = useRoute()

onMounted(async () => {
  try {
    setDisplayTimeZone((await loadAuthConfig()).timeZone)
  } catch {
    setDisplayTimeZone()
  }
})
</script>

<template>
  <el-config-provider :locale="zhCn">
    <router-view v-if="route.meta.public" />
    <AppShell v-else />
  </el-config-provider>
</template>
