<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Database, HardDrive, RefreshCw, ShieldCheck } from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { apiGet } from '../api/client'

interface SystemInfo { name: string; adminMode: string; database: string; cache: string; apiVersion: string }
const loading = ref(false)
const info = ref<SystemInfo>()

async function load() {
  loading.value = true
  try { info.value = await apiGet<SystemInfo>('/system') } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}
onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page-stack settings-page">
    <div class="page-toolbar"><div class="muted">当前实例的运行模式与依赖状态</div><div class="spacer" /><el-button :icon="RefreshCw" @click="load">刷新</el-button></div>
    <section class="settings-band auth-band">
      <ShieldCheck :size="22" />
      <div><strong>Casdoor 单点登录已启用</strong><span>管理端访问由统一身份和用户组策略保护。</span></div>
    </section>
    <section class="settings-band">
      <Database :size="21" />
      <div class="settings-title"><strong>数据存储</strong><span>业务事实与用量账本</span></div>
      <div class="settings-value"><span>{{ info?.database || '—' }}</span><small>已配置</small></div>
    </section>
    <section class="settings-band">
      <HardDrive :size="21" />
      <div class="settings-title"><strong>缓存与调度状态</strong><span>密钥缓存、故障计数与渠道冷却</span></div>
      <div class="settings-value"><span>{{ info?.cache || '—' }}</span><small>已配置</small></div>
    </section>
    <section class="settings-grid panel">
      <div><span>产品</span><strong>{{ info?.name || '—' }}</strong></div>
      <div><span>管理模式</span><strong>{{ info?.adminMode || '—' }}</strong></div>
      <div><span>中转 API</span><strong>{{ info?.apiVersion || '—' }}</strong></div>
      <div><span>支持范围</span><strong>OpenAI 文本核心</strong></div>
    </section>
  </div>
</template>

<style scoped>
.settings-page { width: 100%; }.settings-band { display: grid; min-height: 78px; grid-template-columns: auto 1fr auto; align-items: center; gap: 14px; padding: 16px 18px; border: 1px solid #dce2e7; border-radius: 6px; background: #fff; }.auth-band { grid-template-columns: auto 1fr; border-color: #acd7cc; color: #126a59; background: #effaf7; }.auth-band div, .settings-title, .settings-value { display: flex; flex-direction: column; gap: 3px; }.auth-band span, .settings-title span, .settings-value small { color: #66717d; font-size: 11px; }.settings-value { align-items: flex-end; }.settings-value span { font-family: 'JetBrains Mono', monospace; font-size: 13px; text-transform: uppercase; }.settings-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; padding: 0; overflow: hidden; background: #dce2e7; }.settings-grid div { display: flex; min-height: 72px; flex-direction: column; gap: 6px; padding: 15px; background: #fff; }.settings-grid span { color: #66717d; font-size: 11px; }.settings-grid strong { font-size: 13px; }@media (max-width: 600px) { .settings-band { grid-template-columns: auto 1fr; }.settings-value { grid-column: 2; align-items: flex-start; }.settings-grid { grid-template-columns: 1fr; } }
</style>
