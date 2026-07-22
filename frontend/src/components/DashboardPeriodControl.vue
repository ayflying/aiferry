<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { CalendarRange } from '@lucide/vue'
import {
  customDashboardPeriod,
  dashboardDatesForPeriod,
  dashboardPresetDays,
  type DashboardPeriod,
} from '../lib/dashboard-range'

type DateRange = [string, string]

const props = defineProps<{ modelValue: DashboardPeriod }>()
const emit = defineEmits<{
  'update:modelValue': [value: DashboardPeriod]
  invalid: [message: string]
}>()

const customOpen = ref(false)
const customDates = ref<DateRange>()
const presetOptions = dashboardPresetDays.map((days) => ({ label: `${days} 天`, value: days }))
const selectedPreset = computed(() => props.modelValue.kind === 'preset' ? props.modelValue.days : undefined)

watch(() => props.modelValue, (period) => {
  if (period.kind === 'custom') {
    customDates.value = [period.startAt, period.endAt]
    customOpen.value = true
  }
}, { immediate: true })

function selectPreset(days: number) {
  customOpen.value = false
  emit('update:modelValue', { kind: 'preset', days: days as typeof dashboardPresetDays[number] })
}

function openCustom() {
  customDates.value = dashboardDatesForPeriod(props.modelValue)
  customOpen.value = true
}

function applyCustom(dates: DateRange | undefined) {
  if (!dates) return
  const period = customDashboardPeriod(dates)
  if (typeof period === 'string') {
    emit('invalid', period)
    return
  }
  emit('update:modelValue', period)
}

function disableFutureDate(value: Date) {
  return value.getTime() > new Date().setHours(0, 0, 0, 0)
}
</script>

<template>
  <div class="dashboard-period-control">
    <el-segmented :model-value="selectedPreset" :options="presetOptions" aria-label="预设时间范围" @change="selectPreset(Number($event))" />
    <el-button :type="customOpen ? 'primary' : 'default'" :icon="CalendarRange" @click="openCustom">自定义</el-button>
    <el-date-picker
      v-if="customOpen"
      v-model="customDates"
      type="daterange"
      value-format="YYYY-MM-DD"
      format="YYYY-MM-DD"
      range-separator="至"
      start-placeholder="开始日期"
      end-placeholder="结束日期"
      unlink-panels
      :disabled-date="disableFutureDate"
      aria-label="自定义仪表盘时间范围"
      @change="applyCustom"
    />
  </div>
</template>

<style scoped>
.dashboard-period-control { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: 8px; }
@media (max-width: 650px) { .dashboard-period-control { width: 100%; }.dashboard-period-control :deep(.el-date-editor) { width: 100%; } }
</style>
