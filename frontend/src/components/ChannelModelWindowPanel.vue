<script setup lang="ts">
import { computed, ref } from 'vue'
import { Search } from '@lucide/vue'

import type { TimeWindow } from '../api/types'
import { createClosedWindow, describeTimeWindow, timeWindowIsEmpty } from '../lib/time-window'
import TimeWindowEditor from './TimeWindowEditor.vue'

/**
 * 渠道「关闭时间」页签：按「渠道 × 模型」配置定时关闭时段。
 *
 * 选中模型并设置时段后，这些模型在该时段内不再通过本渠道提供；时段过去自动恢复。
 * 关闭判定在网关路由阶段实时进行，只影响本渠道，其他渠道仍可正常服务同一个模型。
 *
 * windows 的语义：键存在即「本次提交了该字段」——值为空时间窗表示清除，
 * 键不存在表示保持库里原值。这样才能区分「没配过」与「显式清空」。
 */
const props = defineProps<{
  models: Array<{ publicName: string; upstreamName: string }>
  windows: Record<string, TimeWindow>
}>()

const emit = defineEmits<{ (event: 'update:windows', value: Record<string, TimeWindow>): void }>()

const keyword = ref('')
const editing = ref('')

const visibleModels = computed(() => {
  const trimmed = keyword.value.trim().toLowerCase()
  if (!trimmed) return props.models
  return props.models.filter((item) =>
    item.publicName.toLowerCase().includes(trimmed) || item.upstreamName.toLowerCase().includes(trimmed))
})

const closedModels = computed(() => props.models.filter((item) => hasWindow(item.publicName)))

function hasWindow(publicName: string): boolean {
  const window = props.windows[publicName]
  return Boolean(window) && !timeWindowIsEmpty(window)
}

function summaryOf(publicName: string): string {
  return hasWindow(publicName) ? describeTimeWindow(props.windows[publicName]) : '未关闭'
}

function toggle(publicName: string, enabled: boolean) {
  const next = { ...props.windows }
  if (enabled) {
    next[publicName] = createClosedWindow()
    editing.value = publicName
  } else {
    // 置空对象而不是删键：删键后端会「保持原值」，无法表达清除。
    next[publicName] = { tz: '', weekdays: [], ranges: [] }
    if (editing.value === publicName) editing.value = ''
  }
  emit('update:windows', next)
}

function updateWindow(publicName: string, value: TimeWindow) {
  emit('update:windows', { ...props.windows, [publicName]: value })
}
</script>

<template>
  <div class="window-panel">
    <p class="panel-hint">
      打开某个模型的开关并设置时段后，该模型在时段内不再通过本渠道提供服务，时段过去自动恢复。
      <strong>只影响本渠道</strong>：其他渠道仍可正常服务同一个模型。
    </p>

    <div v-if="!models.length" class="panel-empty">当前没有启用的模型，请先在「选择模型」页签勾选模型。</div>

    <template v-else>
      <div class="panel-toolbar">
        <el-input v-model="keyword" clearable placeholder="搜索模型" :prefix-icon="Search" />
        <span class="panel-count">已关闭 {{ closedModels.length }} / {{ models.length }}</span>
      </div>

      <div class="window-list">
        <div v-for="item in visibleModels" :key="item.publicName" class="window-entry">
          <div class="window-entry__head">
            <div class="window-entry__title">
              <code>{{ item.publicName }}</code>
              <span v-if="item.upstreamName !== item.publicName">{{ item.upstreamName }}</span>
            </div>
            <span class="window-entry__summary" :class="{ active: hasWindow(item.publicName) }">{{ summaryOf(item.publicName) }}</span>
            <el-switch :model-value="hasWindow(item.publicName)" @update:model-value="toggle(item.publicName, $event)" />
            <el-button text size="small" :disabled="!hasWindow(item.publicName)" @click="editing = editing === item.publicName ? '' : item.publicName">
              {{ editing === item.publicName ? '收起' : '编辑时段' }}
            </el-button>
          </div>
          <TimeWindowEditor
            v-if="editing === item.publicName"
            :model-value="props.windows[item.publicName] ?? createClosedWindow()"
            class="window-entry__editor"
            @update:model-value="updateWindow(item.publicName, $event)"
          />
        </div>
        <div v-if="!visibleModels.length" class="panel-empty">没有匹配的模型。</div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.window-panel { display: grid; gap: 10px; }.panel-hint { margin: 0; color: #66717d; font-size: 11px; line-height: 1.6; }.panel-hint strong { color: #33404c; }.panel-empty { padding: 14px; border: 1px dashed #dce2e7; border-radius: 6px; color: #66717d; font-size: 12px; text-align: center; }.panel-toolbar { display: flex; align-items: center; gap: 10px; }.panel-toolbar .el-input { max-width: 260px; }.panel-count { color: #66717d; font-size: 11px; }.window-list { display: grid; gap: 7px; max-height: 420px; overflow-y: auto; }.window-entry { display: grid; gap: 8px; padding: 9px 11px; border: 1px solid #dce2e7; border-radius: 6px; }.window-entry__head { display: flex; align-items: center; gap: 10px; }.window-entry__title { display: flex; min-width: 0; flex: 1; align-items: baseline; gap: 8px; }.window-entry__title code { overflow: hidden; color: #15202b; font-size: 12px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }.window-entry__title span { overflow: hidden; color: #66717d; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.window-entry__summary { flex: 0 0 auto; color: #66717d; font-size: 11px; }.window-entry__summary.active { color: #b45309; font-weight: 600; }.window-entry__editor { padding: 9px; border: 1px solid #e3e8ec; border-radius: 6px; background: #fbfcfd; }
</style>
