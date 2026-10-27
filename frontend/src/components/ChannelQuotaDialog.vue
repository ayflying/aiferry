<script setup lang="ts">
import { computed } from 'vue'

import type { ChannelQuotaResult, ChannelQuotaWindow } from '../api/types'
import { formatTime } from '../lib/format'

const props = defineProps<{
  modelValue: boolean
  channelName: string
  loading: boolean
  error: string
  result: ChannelQuotaResult | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  refresh: []
}>()

const open = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const levelLabel = computed(() => {
  const level = props.result?.level.trim().toUpperCase()
  return level ? `套餐档位：${level}` : '套餐档位未知'
})

// 用量越高越接近限额，因此按已用百分比给绿 / 橙 / 红三档颜色。
function windowColor(window: ChannelQuotaWindow) {
  if (window.usedPercent >= 90) return '#f56c6c'
  if (window.usedPercent >= 60) return '#e6a23c'
  return '#67c23a'
}

function windowDetail(window: ChannelQuotaWindow) {
  // 计数型窗口（如「每日签到」）表达的是「N/M 个账号已完成」，不是用量额度：
  // 用 完成数/总数 直接说人话，避免让人把它读成一笔可用余额。
  if (window.counts) {
    const done = Math.round(window.doneCount ?? 0)
    const accounts = Math.round(window.accountCount ?? 0)
    if (accounts === 0) return '暂无账号'
    return `今天已有 ${done} / ${accounts} 个账号完成签到`
  }
  // 百分比统一保留两位小数，避免超长小数串（如 4.554234857142856%）。
  const percent = window.usedPercent.toFixed(2)
  if (window.used !== undefined || window.total !== undefined || window.remaining !== undefined) {
    const used = window.used === undefined ? '—' : Math.round(window.used)
    const total = window.total === undefined ? '—' : Math.round(window.total)
    const remaining = window.remaining === undefined ? '—' : Math.round(window.remaining)
    return `已用 ${used} / ${total}，剩余 ${remaining}（${percent}%）`
  }
  return `已用 ${percent}%`
}

function resetLabel(window: ChannelQuotaWindow) {
  if (!window.nextResetAt) return ''
  return `重置于 ${formatTime(window.nextResetAt)}`
}

// windowValue 是进度条右侧的数值标签。计数型窗口显示「已完成/总数」而不是百分比：
// 「1 / 2」比「50%」更能说明今天两个账号里只签了一个。
function windowValue(window: ChannelQuotaWindow) {
  if (window.counts) {
    return `${Math.round(window.doneCount ?? 0)} / ${Math.round(window.accountCount ?? 0)}`
  }
  return ''
}
</script>

<template>
  <el-dialog v-model="open" :title="`套餐额度 · ${channelName}`" width="min(460px, 94vw)">
    <div v-loading="props.loading" class="quota-dialog__body">
      <el-alert v-if="props.error" :title="props.error" type="error" :closable="false" show-icon />
      <template v-else-if="props.result">
        <div class="quota-dialog__meta">
          <span>{{ levelLabel }}</span>
          <small v-if="props.result.cached" class="muted">1 分钟内缓存，点击刷新可强制更新</small>
          <small v-else class="muted">{{ formatTime(props.result.queriedAt) }}</small>
        </div>
        <el-alert
          v-if="props.result.partialErrors?.length"
          type="warning"
          :closable="false"
          show-icon
          :title="`有 ${props.result.partialErrors.length} 把密钥查询失败，以下为成功密钥的合并结果`"
        >
          <div v-for="message in props.result.partialErrors" :key="message" class="quota-dialog__partial">{{ message }}</div>
        </el-alert>
        <div v-for="window in props.result.windows" :key="window.kind" class="quota-window">
          <div class="quota-window__header"><strong>{{ window.label }}</strong><span class="muted">{{ resetLabel(window) }}</span></div>
          <el-progress :percentage="Math.min(100, Math.max(0, Number(window.usedPercent.toFixed(2))))" :color="windowColor(window)" :stroke-width="14" :format="() => windowValue(window)" />
          <div class="quota-window__detail">{{ windowDetail(window) }}</div>
        </div>
      </template>
      <div v-else class="muted">暂无数据</div>
    </div>
    <template #footer>
      <el-button @click="emit('refresh')" :loading="props.loading">刷新</el-button>
      <el-button type="primary" @click="open = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.quota-dialog__body { display: flex; min-height: 120px; flex-direction: column; gap: 16px; }
.quota-dialog__meta { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.quota-dialog__partial { font-size: 12px; margin-top: 4px; }
.quota-window { display: flex; flex-direction: column; gap: 6px; }
.quota-window__header { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.quota-window__header strong { font-size: 13px; }
.quota-window__detail { color: #66717d; font-size: 12px; }
</style>
