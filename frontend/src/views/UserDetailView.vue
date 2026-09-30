<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { init, use } from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { CanvasRenderer } from 'echarts/renderers'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import type { EChartsType } from 'echarts/core'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, Eye, EyeOff, RefreshCw, UserRound } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { APIKey, Dashboard, ManagedUser } from '../api/types'
import UsageView from './UsageView.vue'
import { showError } from '../lib/error'
import { formatBalance, formatCost, formatNumber, formatTime } from '../lib/format'

use([CanvasRenderer, BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent])

const route = useRoute()
const router = useRouter()
const activeTab = ref('dashboard')
const user = ref<ManagedUser>()
const keys = ref<APIKey[]>([])
const dashboard = ref<Dashboard>()
const loading = ref(false)
const revealedSecrets = reactive<Record<number, string>>({})
const secretLoading = reactive<Record<number, boolean>>({})
const userId = computed(() => String(route.params.id))
const summary = computed(() => dashboard.value?.summary)
const chartElement = ref<HTMLDivElement>()
const costChartElement = ref<HTMLDivElement>()
let chart: EChartsType | undefined
let costChart: EChartsType | undefined

async function load() {
  loading.value = true
  try {
    const [users, userKeys, dashboardData] = await Promise.all([
      apiGet<ManagedUser[]>('/users'),
      apiGet<APIKey[]>('/api-keys', { userId: userId.value }),
      apiGet<Dashboard>(`/users/${encodeURIComponent(userId.value)}/dashboard`, { days: 30 }),
    ])
    user.value = users.find((item) => String(item.id) === userId.value)
    if (!user.value) throw new Error('用户不存在')
    keys.value = userKeys
    dashboard.value = dashboardData
    await nextTick()
    renderChart()
    renderCostChart()
  } catch (error) { showError(error, '加载用户详情失败') } finally { loading.value = false }
}

function renderChart() {
  if (!chartElement.value || !dashboard.value) return
  chart ||= init(chartElement.value)
  const hourly = dashboard.value.trendBucketUnit === 'hour'
  chart.setOption({
    animationDuration: 450,
    color: ['#1677ff', '#16866f'],
    grid: { top: 36, right: 54, bottom: 32, left: 48, containLabel: true },
    tooltip: { trigger: 'axis' },
    legend: { top: 2, left: 'center' },
    xAxis: { type: 'category', data: dashboard.value.trend.map(point => hourly ? point.bucket.slice(5, 16) : point.bucket.slice(5)), axisLabel: { hideOverlap: true, interval: hourly ? 2 : 'auto' } },
    yAxis: [{ type: 'value', name: '请求' }, { type: 'value', name: 'Token' }],
    series: [
      { name: '请求', type: 'bar', barMaxWidth: hourly ? 16 : 22, data: dashboard.value.trend.map(point => point.requests) },
      { name: 'Token', type: 'line', yAxisIndex: 1, smooth: !hourly, showSymbol: !hourly, data: dashboard.value.trend.map(point => point.inputTokens + point.outputTokens) },
    ],
  })
}

function renderCostChart() {
  if (!costChartElement.value || !dashboard.value) return
  const points = dashboard.value.trend
  if (!points.length) {
    costChart?.dispose()
    costChart = undefined
    return
  }
  costChart ||= init(costChartElement.value)
  const hourly = dashboard.value.trendBucketUnit === 'hour'
  costChart.setOption({
    animationDuration: 450,
    color: ['#16866f'],
    grid: { top: 22, right: 24, bottom: 32, left: 58, containLabel: true },
    tooltip: { trigger: 'axis', valueFormatter: (value: number | string) => formatCost(Number(value)) },
    xAxis: { type: 'category', data: points.map(point => hourly ? point.bucket.slice(5, 16) : point.bucket.slice(5)), axisLabel: { hideOverlap: true, interval: hourly ? 2 : 'auto' } },
    yAxis: { type: 'value', name: '成本', axisLabel: { formatter: (value: number) => formatCost(value) } },
    series: [{ name: '估算成本', type: 'line', smooth: !hourly, showSymbol: !hourly, areaStyle: { opacity: 0.2 }, data: points.map(point => point.estimatedCost ?? 0) }],
  })
}

function resizeChart() { chart?.resize(); costChart?.resize() }

async function toggleSecret(item: APIKey) {
  if (!item.secretAvailable || secretLoading[item.id]) return
  if (revealedSecrets[item.id]) { delete revealedSecrets[item.id]; return }
  secretLoading[item.id] = true
  try { revealedSecrets[item.id] = (await apiGet<{ key: string }>(`/api-keys/${item.id}/secret`)).key }
  catch (error) { showError(error, '显示完整密钥失败') }
  finally { secretLoading[item.id] = false }
}

function selectTab(tab: string) {
  activeTab.value = tab
  if (tab === 'dashboard') {
    void nextTick(() => {
      if (chart) chart.resize()
      else renderChart()
      if (costChart) costChart.resize()
      else renderCostChart()
    })
  }
}

watch(dashboard, async () => { await nextTick(); renderChart(); renderCostChart() })
onMounted(() => { load(); window.addEventListener('resize', resizeChart) })
onBeforeUnmount(() => { window.removeEventListener('resize', resizeChart); chart?.dispose(); costChart?.dispose() })
</script>

<template>
  <div v-loading="loading" class="page-stack">
    <div class="page-toolbar"><el-button :icon="ArrowLeft" @click="router.push('/users')">返回用户管理</el-button><div class="spacer" /><el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button></div>
    <section v-if="user" class="table-panel user-summary">
      <el-avatar :size="44" :src="user.avatarUrl || undefined"><UserRound :size="20" /></el-avatar>
      <div><h2>{{ user.nickname }}</h2><span class="muted">{{ user.role === 'admin' ? '管理员' : '用户' }} · {{ user.email || '未绑定邮箱' }}</span></div>
      <div class="spacer" /><div class="summary-balance"><small>账户余额</small><strong>{{ formatBalance(user.balance) }}</strong></div>
    </section>
    <el-tabs :model-value="activeTab" class="detail-tabs" @tab-change="(tab: string | number) => selectTab(String(tab))">
      <el-tab-pane label="用户仪表盘" name="dashboard">
        <section v-if="summary" class="metric-grid">
          <article class="metric-card"><div class="label">总请求</div><div class="value">{{ formatNumber(summary.requests) }}</div><div class="detail">最近 30 天</div></article>
          <article class="metric-card"><div class="label">成功请求</div><div class="value">{{ formatNumber(summary.successes) }}</div><div class="detail">输入 {{ formatNumber(summary.inputTokens) }} · 输出 {{ formatNumber(summary.outputTokens) }}</div></article>
          <article class="metric-card"><div class="label">总 Token</div><div class="value">{{ formatNumber(summary.totalTokens) }}</div><div class="detail">近 30 天用量</div></article>
          <article class="metric-card"><div class="label">估算成本</div><div class="value">{{ formatCost(summary.estimatedCost) }}</div><div class="detail">近 30 天</div></article>
        </section>
        <section v-if="dashboard" class="table-panel chart-panel"><div class="section-heading"><div><h2>请求与 Token 趋势</h2><p>最近 30 天 · {{ dashboard.trendBucketUnit === 'hour' ? '按小时聚合' : '按天聚合' }}</p></div></div><div ref="chartElement" class="trend-chart" /></section>
        <section v-if="dashboard" class="table-panel chart-panel"><div class="section-heading"><div><h2>消费趋势</h2><p>最近 30 天 · 每 {{ dashboard.trendBucketUnit === 'hour' ? '小时' : '日' }}估算成本</p></div></div><div ref="costChartElement" class="trend-chart cost-trend-chart" /></section>
        <section v-if="dashboard" class="dashboard-grid">
          <div class="table-panel"><div class="section-heading"><h2>模型用量</h2></div><el-table :data="dashboard.byModel" row-key="name"><el-table-column prop="name" label="模型" min-width="150"/><el-table-column prop="requests" label="请求数" align="right"/><el-table-column label="Token" align="right"><template #default="{ row }">{{ formatNumber(row.totalTokens) }}</template></el-table-column><el-table-column label="成本" align="right"><template #default="{ row }">{{ formatCost(row.estimatedCost) }}</template></el-table-column></el-table></div>
          <div class="table-panel"><div class="section-heading"><h2>渠道请求</h2></div><el-table :data="dashboard.byChannel" row-key="name"><el-table-column prop="name" label="渠道" min-width="140"/><el-table-column prop="requests" label="请求数" align="right"/><el-table-column label="Token" align="right"><template #default="{ row }">{{ formatNumber(row.totalTokens) }}</template></el-table-column><el-table-column label="成本" align="right"><template #default="{ row }">{{ formatCost(row.estimatedCost) }}</template></el-table-column></el-table></div>
        </section>
        <div v-if="dashboard && !dashboard.summary.requests" class="empty-state">最近 30 天暂无用量</div>
      </el-tab-pane>
      <el-tab-pane label="模型使用日志" name="usage">
        <UsageView v-if="user" :user-id="user.id" class="embedded-usage" />
      </el-tab-pane>
      <el-tab-pane :label="`访问密钥（${keys.length}）`" name="keys">
        <div class="table-panel"><el-table :data="keys" row-key="id">
          <el-table-column label="名称" prop="name" min-width="150"/><el-table-column label="密钥" min-width="280"><template #default="{ row }"><div class="key-cell"><span class="mono key-value">{{ revealedSecrets[row.id] || `${row.keyPrefix}••••` }}</span><el-button text circle :icon="revealedSecrets[row.id] ? EyeOff : Eye" :loading="secretLoading[row.id]" :disabled="!row.secretAvailable" :aria-label="revealedSecrets[row.id] ? '隐藏完整密钥' : '显示完整密钥'" :title="row.secretAvailable ? (revealedSecrets[row.id] ? '隐藏完整密钥' : '显示完整密钥') : '该密钥无法恢复'" @click="toggleSecret(row)"/></div></template></el-table-column>
          <el-table-column label="状态" width="90"><template #default="{ row }"><el-tag :type="row.status === 1 ? 'success' : 'info'">{{ row.status === 1 ? '启用' : '停用' }}</el-tag></template></el-table-column><el-table-column label="已用额度" min-width="110" align="right"><template #default="{ row }">{{ formatBalance(row.spentAmount) }}</template></el-table-column><el-table-column label="最近使用" min-width="160"><template #default="{ row }">{{ row.lastUsedAt ? formatTime(row.lastUsedAt) : '从未使用' }}</template></el-table-column>
        </el-table><div v-if="!loading && !keys.length" class="empty-state">该用户尚未申请访问密钥</div></div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped>
.user-summary { display: flex; align-items: center; gap: 14px; padding: 18px; }
.user-summary h2, .section-heading h2 { margin: 0 0 5px; font-size: 16px; }
.summary-balance { display: flex; flex-direction: column; gap: 4px; text-align: right; }
.summary-balance small, .section-heading { color: #7b8792; font-size: 12px; }
.section-heading { padding: 16px 18px; }
.detail-tabs { min-width: 0; }
.chart-panel { overflow: hidden; }
.trend-chart { width: 100%; height: 300px; }
.cost-trend-chart { height: 280px; }
.section-heading p { margin: 4px 0 0; color: #7b8792; font-size: 12px; }
.dashboard-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.key-cell { display: flex; align-items: center; gap: 8px; min-width: 0; }
.key-value { min-width: 0; overflow-wrap: anywhere; }
.empty-state { padding: 28px; text-align: center; color: #7b8792; }
.embedded-usage { width: 100%; }
@media (max-width: 760px) { .trend-chart, .cost-trend-chart { height: 250px; }.dashboard-grid { grid-template-columns: 1fr; } }
</style>
