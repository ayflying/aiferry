<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Database, Gauge, HardDrive, RefreshCw, Route, ShieldCheck, Stethoscope } from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { apiGet, apiPut } from '../api/client'
import type { SystemResilienceSettings } from '../api/types'

interface SystemInfo { name: string; adminMode: string; database: string; cache: string; apiVersion: string }
type SettingsTab = 'overview' | 'routing' | 'health' | 'autoDisable'

const loading = ref(false)
const saving = ref(false)
const activeTab = ref<SettingsTab>('overview')
const info = ref<SystemInfo>()
const form = reactive({
  maxFailoverAttempts: 3,
  retryStatusCodes: '401,403,404,408,429,500-599',
  healthCheckEnabled: false,
  healthCheckMode: 'passive' as SystemResilienceSettings['healthCheckMode'],
  healthCheckIntervalMinutes: 5,
  recoveryEnabled: true,
  autoDisableEnabled: true,
  disableLatencySeconds: 120,
  disableStatusCodes: '401,429',
  failureKeywordsText: '',
})

const sectionMeta = computed(() => ({
  overview: { title: '运行概览', description: '实例与依赖状态' },
  routing: { title: '路由可靠性', description: '故障转移范围与尝试次数' },
  health: { title: '渠道健康检查', description: '后台探测与自动恢复' },
  autoDisable: { title: '自动禁用规则', description: '上游异常时下线渠道' },
}[activeTab.value]))

function applySettings(settings: SystemResilienceSettings) {
  Object.assign(form, {
    ...settings,
    healthCheckMode: settings.healthCheckMode,
    failureKeywordsText: settings.failureKeywords.join('\n'),
  })
}

async function load() {
  loading.value = true
  try {
    const [system, settings] = await Promise.all([apiGet<SystemInfo>('/system'), apiGet<SystemResilienceSettings>('/system/settings')])
    info.value = system
    applySettings(settings)
  } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    const settings = await apiPut<SystemResilienceSettings>('/system/settings', {
      maxFailoverAttempts: form.maxFailoverAttempts,
      retryStatusCodes: form.retryStatusCodes,
      healthCheckEnabled: form.healthCheckEnabled,
      healthCheckMode: form.healthCheckMode,
      healthCheckIntervalMinutes: form.healthCheckIntervalMinutes,
      recoveryEnabled: form.recoveryEnabled,
      autoDisableEnabled: form.autoDisableEnabled,
      disableLatencySeconds: form.disableLatencySeconds,
      disableStatusCodes: form.disableStatusCodes,
      failureKeywords: form.failureKeywordsText.split('\n').map((item) => item.trim()).filter(Boolean),
    })
    applySettings(settings)
    ElMessage.success('系统设置已保存')
  } catch (error) { ElMessage.error((error as Error).message) } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page-stack settings-page">
    <el-tabs v-model="activeTab" class="settings-tabs">
      <el-tab-pane name="overview"><template #label><span class="tab-label"><Activity :size="15" />概览</span></template></el-tab-pane>
      <el-tab-pane name="routing"><template #label><span class="tab-label"><Route :size="15" />路由</span></template></el-tab-pane>
      <el-tab-pane name="health"><template #label><span class="tab-label"><Stethoscope :size="15" />健康检查</span></template></el-tab-pane>
      <el-tab-pane name="autoDisable"><template #label><span class="tab-label"><ShieldCheck :size="15" />自动禁用</span></template></el-tab-pane>
    </el-tabs>

    <div class="page-toolbar settings-toolbar">
      <div><h1>{{ sectionMeta.title }}</h1><span class="muted">{{ sectionMeta.description }}</span></div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
      <el-button v-if="activeTab !== 'overview'" type="primary" :loading="saving" @click="save">保存更改</el-button>
    </div>

    <template v-if="activeTab === 'overview'">
      <section class="settings-band auth-band"><ShieldCheck :size="22" /><div><strong>Casdoor 单点登录已启用</strong><span>管理端访问由统一身份与用户组策略保护。</span></div></section>
      <section class="settings-band"><Database :size="21" /><div class="settings-title"><strong>数据存储</strong><span>业务事实与用量账本</span></div><div class="settings-value"><span>{{ info?.database || '—' }}</span><small>已配置</small></div></section>
      <section class="settings-band"><HardDrive :size="21" /><div class="settings-title"><strong>缓存与临时状态</strong><span>密钥缓存、故障计数与短时冷却</span></div><div class="settings-value"><span>{{ info?.cache || '—' }}</span><small>已配置</small></div></section>
      <section class="settings-grid"><div><span>产品</span><strong>{{ info?.name || '—' }}</strong></div><div><span>管理模式</span><strong>{{ info?.adminMode || '—' }}</strong></div><div><span>中转 API</span><strong>{{ info?.apiVersion || '—' }}</strong></div><div><span>支持范围</span><strong>OpenAI 文本核心</strong></div></section>
    </template>

    <template v-else-if="activeTab === 'routing'">
      <section class="settings-section"><div class="section-heading"><div><h2>故障转移</h2><span>单次请求会按路由顺序切换不同渠道</span></div><Gauge :size="19" /></div><el-form label-position="top" class="settings-form"><div class="form-grid"><el-form-item label="最大尝试渠道数"><el-input-number v-model="form.maxFailoverAttempts" :min="1" :max="10" controls-position="right" /></el-form-item><el-form-item label="可故障转移状态码"><el-input v-model="form.retryStatusCodes" placeholder="401,429,500-599" /></el-form-item></div><p class="field-hint">状态码支持逗号分隔和包含范围，例如 401,429,500-599。</p></el-form></section>
    </template>

    <template v-else-if="activeTab === 'health'">
      <section class="settings-section"><div class="section-heading"><div><h2>后台渠道探测</h2><span>使用已启用模型执行最小请求，不保存提示词或响应正文。</span></div><el-switch v-model="form.healthCheckEnabled" /></div><el-form label-position="top" class="settings-form"><div class="form-grid"><el-form-item label="检查范围"><el-segmented v-model="form.healthCheckMode" :disabled="!form.healthCheckEnabled" :options="[{ label: '仅恢复自动禁用渠道', value: 'passive' }, { label: '检查全部可管理渠道', value: 'all' }]" /></el-form-item><el-form-item label="检查间隔（分钟）"><el-input-number v-model="form.healthCheckIntervalMinutes" :disabled="!form.healthCheckEnabled" :min="1" :max="1440" controls-position="right" /></el-form-item></div></el-form></section>
      <section class="settings-section compact"><div class="setting-switch"><div><strong>成功后自动恢复</strong><span>只恢复有自动禁用标记的渠道，手动停用保持不变。</span></div><el-switch v-model="form.recoveryEnabled" /></div></section>
    </template>

    <template v-else>
      <section class="settings-section"><div class="section-heading"><div><h2>上游异常自动下线</h2><span>命中任一规则后渠道停止参与路由，并保存触发原因。</span></div><el-switch v-model="form.autoDisableEnabled" /></div><el-form label-position="top" class="settings-form"><div class="form-grid"><el-form-item label="慢响应阈值（秒）"><el-input-number v-model="form.disableLatencySeconds" :disabled="!form.autoDisableEnabled" :min="1" :max="3600" controls-position="right" /></el-form-item><el-form-item label="自动禁用状态码"><el-input v-model="form.disableStatusCodes" :disabled="!form.autoDisableEnabled" placeholder="401,429" /></el-form-item></div><el-form-item label="失败关键词"><el-input v-model="form.failureKeywordsText" :disabled="!form.autoDisableEnabled" type="textarea" :rows="10" spellcheck="false" placeholder="每行一个关键词" /></el-form-item><p class="field-hint">关键词不区分大小写；状态码支持逗号分隔和包含范围。</p></el-form></section>
    </template>
  </div>
</template>

<style scoped>
.settings-page { width: 100%; }.settings-tabs :deep(.el-tabs__header) { margin: 0; }.settings-tabs :deep(.el-tabs__nav-wrap::after) { background: #dce2e7; }.tab-label { display: inline-flex; align-items: center; gap: 7px; }.settings-toolbar { display: flex; min-height: 54px; align-items: center; gap: 10px; padding: 16px 0 18px; border-bottom: 1px solid #dce2e7; }.settings-toolbar h1 { margin: 0 0 4px; color: #15202b; font-size: 16px; font-weight: 650; }.spacer { flex: 1; }.settings-band { display: grid; min-height: 78px; grid-template-columns: auto 1fr auto; align-items: center; gap: 14px; padding: 16px 0; border-bottom: 1px solid #dce2e7; }.auth-band { color: #126a59; }.auth-band div, .settings-title, .settings-value { display: flex; flex-direction: column; gap: 3px; }.auth-band span, .settings-title span, .settings-value small, .field-hint, .setting-switch span, .section-heading span { color: #66717d; font-size: 11px; }.settings-value { align-items: flex-end; }.settings-value span { font-family: 'JetBrains Mono', monospace; font-size: 13px; text-transform: uppercase; }.settings-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-bottom: 1px solid #dce2e7; }.settings-grid div { display: flex; min-height: 72px; flex-direction: column; justify-content: center; gap: 6px; border-right: 1px solid #dce2e7; }.settings-grid div:last-child { border: 0; }.settings-grid span { color: #66717d; font-size: 11px; }.settings-grid strong { font-size: 13px; }.settings-section { padding: 20px 0; border-bottom: 1px solid #dce2e7; }.settings-section.compact { padding: 18px 0; }.section-heading, .setting-switch { display: flex; align-items: center; justify-content: space-between; gap: 18px; }.section-heading h2 { margin: 0 0 4px; color: #15202b; font-size: 14px; }.settings-form { margin-top: 18px; }.settings-form :deep(.el-form-item) { margin-bottom: 10px; }.settings-form :deep(.el-input-number) { width: 100%; }.setting-switch div { display: flex; flex-direction: column; gap: 4px; }.field-hint { margin: 2px 0 0; line-height: 1.55; }.settings-form :deep(textarea) { font-family: 'JetBrains Mono', monospace; font-size: 12px; }@media (max-width: 720px) { .settings-toolbar { align-items: flex-start; flex-wrap: wrap; }.settings-toolbar .spacer { display: none; }.settings-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }.settings-grid div:nth-child(2) { border-right: 0; }.settings-grid div:nth-child(-n+2) { border-bottom: 1px solid #dce2e7; }.section-heading { align-items: flex-start; }.settings-form :deep(.el-segmented) { max-width: 100%; height: auto; flex-wrap: wrap; } }@media (max-width: 480px) { .settings-grid { grid-template-columns: 1fr; }.settings-grid div { border-right: 0; border-bottom: 1px solid #dce2e7; }.settings-grid div:last-child { border-bottom: 0; } }
</style>
