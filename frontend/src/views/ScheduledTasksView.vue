<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { AlarmClock, RefreshCw } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { ScheduledTask, ScheduledTaskList } from '../api/types/schedule'
import { showError } from '../lib/error'
import { formatTime } from '../lib/format'

const loading = ref(false)
const tasks = ref<ScheduledTask[]>([])
const processStartedAt = ref('')
const loadedAt = ref('')
// 上次刷新距今的秒数，用来把「下次执行」显示成倒计时，页面不会因为时间流逝而失真。
let timer: number | undefined

const enabledTasks = computed(() => tasks.value.filter((task) => task.enabled))
const disabledTasks = computed(() => tasks.value.filter((task) => !task.enabled))
const failedTasks = computed(() => tasks.value.filter((task) => task.enabled && task.lastError))

async function load() {
  loading.value = true
  try {
    const result = await apiGet<ScheduledTaskList>('/scheduled-tasks')
    tasks.value = result.tasks
    processStartedAt.value = result.processStartedAt
    loadedAt.value = result.now
  } catch (error) {
    showError(error, '加载计划任务失败')
  } finally {
    loading.value = false
  }
}

// 有没有「该跑但没跑」的任务：已启用、从未执行、且进程已启动足够久（超过一个周期）。
// 这类任务才是真异常，刚启动几秒的进程不该被误判。
function looksStale(task: ScheduledTask) {
  if (!task.enabled || task.lastStartedAt) return false
  if (!processStartedAt.value) return false
  const started = new Date(processStartedAt.value).getTime()
  if (Number.isNaN(started)) return false
  return Date.now() - started > 15 * 60 * 1000
}

function statusText(task: ScheduledTask) {
  if (!task.enabled) return '未启用'
  if (task.running) return '执行中'
  if (task.lastError) return '上次失败'
  if (!task.lastStartedAt) return '尚未执行'
  return '正常'
}

function statusType(task: ScheduledTask): 'success' | 'warning' | 'danger' | 'info' {
  if (!task.enabled) return 'info'
  if (task.running) return 'warning'
  if (task.lastError) return 'danger'
  return 'success'
}

function durationText(task: ScheduledTask) {
  if (!task.lastFinishedAt) return '—'
  const ms = task.lastDurationMs
  if (ms < 1000) return `${ms} 毫秒`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)} 秒`
  return `${Math.floor(ms / 60000)} 分 ${Math.round((ms % 60000) / 1000)} 秒`
}

// 下次执行显示成「时间 + 还有多久」，比单纯一个时间戳更容易判断任务是否卡住。
function nextRunText(task: ScheduledTask) {
  if (!task.nextRunAt) return '事件驱动'
  const target = new Date(task.nextRunAt).getTime()
  if (Number.isNaN(target)) return '—'
  const remain = target - Date.now()
  if (remain <= 0) return `${formatTime(task.nextRunAt)}（即将执行）`
  return `${formatTime(task.nextRunAt)}（还有 ${relativeText(remain)}）`
}

function relativeText(ms: number) {
  const minutes = Math.floor(ms / 60000)
  if (minutes < 1) return '不到 1 分钟'
  if (minutes < 60) return `${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest > 0 ? `${hours} 小时 ${rest} 分钟` : `${hours} 小时`
}

onMounted(() => {
  void load()
  // 每 30 秒重取一次：定时任务本身就是「随时间变化」的数据，静态快照会误导。
  timer = window.setInterval(() => void load(), 30000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <section class="page-section">
    <div class="page-heading">
      <div>
        <h1>计划任务</h1>
        <span>系统全部后台定时任务的运行状态：调度规则、上次与下次执行时间、耗时和失败原因。</span>
      </div>
      <div class="page-heading__actions">
        <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>

    <el-alert
      v-if="failedTasks.length"
      type="error"
      :closable="false"
      show-icon
      :title="`有 ${failedTasks.length} 个任务上次执行失败，请检查下方的失败原因`"
      style="margin-bottom: 16px"
    />
    <el-alert
      v-if="tasks.some(looksStale)"
      type="warning"
      :closable="false"
      show-icon
      title="有已启用的任务在本次启动后一直没有执行记录，可能是执行被阻塞或周期配置有误"
      style="margin-bottom: 16px"
    />

    <el-table v-loading="loading" :data="tasks" stripe empty-text="暂无计划任务">
      <el-table-column label="任务" min-width="230">
        <template #default="{ row }: { row: ScheduledTask }">
          <div class="task-cell">
            <strong>{{ row.name }}</strong>
            <small>{{ row.description }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="调度" min-width="180">
        <template #default="{ row }: { row: ScheduledTask }">
          <span>{{ row.scheduleText }}</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }: { row: ScheduledTask }">
          <el-tag :type="statusType(row)" size="small">{{ statusText(row) }}</el-tag>
          <div v-if="!row.enabled && row.disabledReason" class="task-reason">{{ row.disabledReason }}</div>
        </template>
      </el-table-column>
      <el-table-column label="上次执行" min-width="170">
        <template #default="{ row }: { row: ScheduledTask }">
          <div>{{ row.lastFinishedAt ? formatTime(row.lastFinishedAt) : '尚未执行' }}</div>
          <small class="muted">耗时 {{ durationText(row) }}</small>
        </template>
      </el-table-column>
      <el-table-column label="下次执行" min-width="190">
        <template #default="{ row }: { row: ScheduledTask }">
          <span>{{ nextRunText(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="累计" width="110" align="center">
        <template #default="{ row }: { row: ScheduledTask }">
          <div>{{ row.runCount }} 次</div>
          <small v-if="row.failureCount" class="muted">失败 {{ row.failureCount }} 次</small>
          <small v-else class="muted">无失败</small>
        </template>
      </el-table-column>
      <el-table-column label="上次结果" min-width="200">
        <template #default="{ row }: { row: ScheduledTask }">
          <span v-if="row.lastError" class="task-error">{{ row.lastError }}</span>
          <span v-else-if="row.enabled && row.lastStartedAt" class="muted">成功</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
    </el-table>

    <div class="task-footer muted">
      <span><AlarmClock :size="14" /> 页面每 30 秒自动刷新</span>
      <span v-if="loadedAt">数据时间 {{ formatTime(loadedAt) }}</span>
      <span v-if="processStartedAt">本次服务启动于 {{ formatTime(processStartedAt) }}（任务计数从启动时算起）</span>
      <span>已启用 {{ enabledTasks.length }} 个，未启用 {{ disabledTasks.length }} 个</span>
    </div>
  </section>
</template>

<style scoped>
.task-cell { display: flex; flex-direction: column; gap: 2px; }
.task-cell small { color: #66717d; font-size: 12px; line-height: 1.45; }
.task-reason { margin-top: 4px; color: #8a949e; font-size: 11px; line-height: 1.4; }
.task-error { color: #f56c6c; font-size: 12px; word-break: break-word; }
.task-footer { display: flex; flex-wrap: wrap; gap: 16px; margin-top: 14px; font-size: 12px; }
.task-footer span { display: inline-flex; align-items: center; gap: 4px; }
</style>
