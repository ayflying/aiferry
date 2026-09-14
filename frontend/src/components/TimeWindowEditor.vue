<script setup lang="ts">
import { computed } from 'vue'
import { Plus, Timer, Trash2 } from '@lucide/vue'

import type { TimeWindow } from '../api/types'
import { createTimeWindow, defaultTimeWindowTimezone, describeTimeWindow, timeWindowWeekdays } from '../lib/time-window'
import { timeZoneOptionGroups } from '../lib/time-zones'
import TableActionButton from './TableActionButton.vue'

/**
 * 时间窗编辑器：时区 + 星期 + 多个每日时段，同时服务「计费规则的生效时段」
 * 与「渠道模型的定时关闭」两处配置。
 *
 * 组件自身不持有状态，始终从 modelValue 渲染、每次修改回抛一个新的 TimeWindow，
 * 因此父级的 v-model 不需要额外的同步守卫，也不会出现「回填 → 回抛」自激。
 */
const props = withDefaults(defineProps<{ modelValue: TimeWindow; disabled?: boolean; showSummary?: boolean }>(), {
  disabled: false,
  showSummary: true,
})

const emit = defineEmits<{ (event: 'update:modelValue', value: TimeWindow): void }>()

const timezone = computed({
  get: () => props.modelValue.tz?.trim() || defaultTimeWindowTimezone,
  set: (value: string) => patch({ tz: value }),
})

const weekdays = computed({
  get: () => props.modelValue.weekdays ?? [],
  set: (value: number[]) => patch({ weekdays: [...value].sort((left, right) => left - right) }),
})

/** 时段行只用于渲染：数据里是 [开始, 结束] 二元组，这里转成具名对象方便模板取值。 */
const rangeRows = computed(() =>
  (props.modelValue.ranges ?? []).map(([start, end]) => ({ start: start ?? '', end: end ?? '' })),
)

const summary = computed(() => describeTimeWindow(props.modelValue))

function patch(changes: Partial<TimeWindow>) {
  emit('update:modelValue', { ...props.modelValue, ...changes })
}

function updateRangeTime(index: number, field: 'start' | 'end', value: string) {
  const rows = rangeRows.value.map((row, current) => (current === index ? { ...row, [field]: value } : row))
  patch({ ranges: toRangeTuples(rows) })
}

function addRange() {
  patch({ ranges: [...toRangeTuples(rangeRows.value), ['09:00', '12:00']] })
}

/** 起止相同在后端语义里就是「全天」，用于表达整日关闭。 */
function addAllDayRange() {
  patch({ ranges: [...toRangeTuples(rangeRows.value), ['00:00', '00:00']] })
}

function removeRange(index: number) {
  patch({ ranges: toRangeTuples(rangeRows.value.filter((_, current) => current !== index)) })
}

function toRangeTuples(rows: Array<{ start: string; end: string }>): Array<[string, string]> {
  return rows.filter((row) => row.start && row.end).map((row) => [row.start, row.end])
}

function applyWeekdays(values: number[]) {
  weekdays.value = values
}
</script>

<template>
  <div class="time-window-editor">
    <div class="time-window-editor__toolbar">
      <el-select v-model="timezone" :disabled="disabled" filterable placeholder="选择时区" class="timezone-select">
        <el-option-group v-for="group in timeZoneOptionGroups" :key="group.label" :label="group.label">
          <el-option v-for="item in group.options" :key="item.value" :label="item.label" :value="item.value" />
        </el-option-group>
      </el-select>
      <el-button-group>
        <el-button size="small" :disabled="disabled" @click="applyWeekdays([1, 2, 3, 4, 5])">工作日</el-button>
        <el-button size="small" :disabled="disabled" @click="applyWeekdays([6, 7])">周末</el-button>
        <el-button size="small" :disabled="disabled" @click="applyWeekdays([])">每天</el-button>
      </el-button-group>
    </div>

    <div class="weekday-row">
      <span class="editor-hint">星期</span>
      <el-checkbox-group v-model="weekdays" :disabled="disabled">
        <el-checkbox-button v-for="item in timeWindowWeekdays" :key="item.value" :value="item.value">{{ item.label }}</el-checkbox-button>
      </el-checkbox-group>
      <span class="editor-hint">不勾选表示不限星期</span>
    </div>

    <div v-for="(row, index) in rangeRows" :key="`range-${index}`" class="range-row">
      <el-time-select :model-value="row.start" :disabled="disabled" start="00:00" end="23:45" step="00:15" :clearable="false" placeholder="开始" @update:model-value="updateRangeTime(index, 'start', $event)" />
      <span class="editor-hint">至</span>
      <el-time-select :model-value="row.end" :disabled="disabled" start="00:00" end="23:45" step="00:15" :clearable="false" placeholder="结束" @update:model-value="updateRangeTime(index, 'end', $event)" />
      <TableActionButton :icon="Trash2" :label="row.start && row.start === row.end ? '删除全天时段' : '删除时段'" danger :size="15" :disabled="disabled" @click="removeRange(index)" />
    </div>

    <div class="range-actions">
      <el-button size="small" :icon="Plus" :disabled="disabled" @click="addRange">添加时段</el-button>
      <el-button size="small" :icon="Timer" :disabled="disabled" @click="addAllDayRange">全天</el-button>
      <span class="editor-hint">支持跨零点（如 22:00 至 06:00）；不添加时段表示不限时刻。</span>
    </div>

    <p v-if="showSummary" class="editor-summary"><span>生效时段</span><strong>{{ summary }}</strong></p>
  </div>
</template>

<style scoped>
.time-window-editor { display: grid; gap: 8px; }.time-window-editor__toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.timezone-select { flex: 1 1 220px; min-width: 180px; }.weekday-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.editor-hint { margin: 0; color: #66717d; font-size: 11px; line-height: 1.5; }.range-row { display: grid; grid-template-columns: 1fr auto 1fr auto; align-items: center; gap: 6px; }.range-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.editor-summary { display: flex; gap: 6px; align-items: baseline; margin: 0; color: #4b5763; font-size: 11px; }.editor-summary span { color: #66717d; }
</style>
