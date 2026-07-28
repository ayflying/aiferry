<script setup lang="ts">
import { RefreshCw, ScanSearch } from '@lucide/vue'
import type { ModelQualityEvent, ModelQualityEventPage, ModelQualitySettings } from '../api/types'
import { formatNumber, formatTime } from '../lib/format'

const props = defineProps<{
  settings: ModelQualitySettings
  events: ModelQualityEventPage
  loading: boolean
  saving: boolean
  page: number
  pageSize: number
}>()

const emit = defineEmits<{
  'update:enabled': [enabled: boolean]
  refresh: []
  'page-change': [page: number]
  'page-size-change': [pageSize: number]
}>()

function reasonLabel(reason: string): string {
  return {
    upstream_model_tier_lower: '上游模型级别降低',
    answer_too_short_for_prompt: '回答长度异常',
  }[reason] || reason
}

function observedModel(event: ModelQualityEvent): string {
  return event.observedModel || '上游未返回模型名'
}
</script>

<template>
  <section class="settings-section">
    <div class="section-heading">
      <div><h2>降智检测</h2><span>仅记录成功文本请求中的可疑结果，不自动处理渠道。</span></div>
      <ScanSearch :size="19" />
    </div>
    <div class="setting-switch">
      <div><strong>启用检测</strong><span>开启后在后台分析，不影响请求响应。</span></div>
      <el-switch :model-value="settings.enabled" :disabled="saving" @update:model-value="emit('update:enabled', $event)" />
    </div>
  </section>

  <section class="settings-section quality-history-section">
    <div class="section-heading">
      <div><h2>检测历史</h2><span>仅保留检测信号和请求元数据。</span></div>
      <el-tooltip content="刷新历史" placement="top"><el-button :icon="RefreshCw" :loading="loading" circle @click="emit('refresh')" /></el-tooltip>
    </div>
    <el-table v-loading="loading" :data="events.items" row-key="id" empty-text="暂无检测历史">
      <el-table-column label="时间" min-width="176"><template #default="{ row }"><span class="mono">{{ formatTime(row.createdAt) }}</span></template></el-table-column>
      <el-table-column label="渠道 / 密钥" min-width="118"><template #default="{ row }"><div class="cell-stack"><strong>{{ row.channelName || '已删除渠道' }}</strong><small>密钥 #{{ row.credentialId }}</small></div></template></el-table-column>
      <el-table-column label="模型" min-width="240"><template #default="{ row }"><div class="cell-stack"><strong>{{ row.requestedModel }}</strong><small>期望 {{ row.expectedModel }} · 实际 {{ observedModel(row) }}</small></div></template></el-table-column>
      <el-table-column label="检测信号" min-width="190"><template #default="{ row }"><div class="reason-list"><el-tag v-for="reason in row.reasons" :key="reason" type="warning" effect="plain" size="small">{{ reasonLabel(reason) }}</el-tag></div></template></el-table-column>
      <el-table-column label="文本长度" min-width="126"><template #default="{ row }"><div class="cell-stack mono"><strong>问 {{ formatNumber(row.questionChars) }}</strong><small>答 {{ formatNumber(row.answerChars) }}</small></div></template></el-table-column>
    </el-table>
    <div class="pagination-row"><el-pagination :current-page="page" :page-size="pageSize" :page-sizes="[20, 50, 100]" :total="events.total" layout="total, sizes, prev, pager, next" @current-change="emit('page-change', $event)" @size-change="emit('page-size-change', $event)" /></div>
  </section>
</template>

<style scoped>
.setting-switch { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding-top: 18px; }.setting-switch > div, .cell-stack { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.setting-switch strong, .cell-stack strong { color: #15202b; font-size: 13px; }.setting-switch span, .cell-stack small { color: #66717d; font-size: 12px; }.quality-history-section { gap: 18px; }.reason-list { display: flex; flex-wrap: wrap; gap: 4px; }.mono { font-family: 'JetBrains Mono', monospace; font-size: 12px; }@media (max-width: 720px) { .setting-switch { align-items: flex-start; flex-direction: column; }.quality-history-section { overflow-x: auto; }.quality-history-section :deep(.el-pagination) { min-width: max-content; } }
</style>
