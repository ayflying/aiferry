<script setup lang="ts">
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { useRoute } from 'vue-router'
import AppShell from './components/AppShell.vue'

const route = useRoute()
</script>

<template>
  <el-config-provider :locale="zhCn">
    <router-view v-if="route.meta.public" />
    <AppShell v-else />
  </el-config-provider>
</template>
