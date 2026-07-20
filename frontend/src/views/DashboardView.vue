<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { init, use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import type { EChartsType } from 'echarts/core'
import { Activity, CircleDollarSign, Clock3, RefreshCw, Route } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { Dashboard } from '../api/types'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { formatCost, formatNumber, successRate } from '../lib/format'
import RouteStrip from '../components/RouteStrip.vue'

use([CanvasRenderer, BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent])

const store = useAppStore()
const loading = ref(false)
const days = ref(7)
const dashboard = ref<Dashboard>({
  summary: { requests: 0, successes: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, averageLatency: 0 },
  trend: [], byModel: [], byChannel: [], recentCost: { totalEstimatedCost: 0, models: [] },
})
const chartElement = ref<HTMLDivElement>()
const costChartElement = ref<HTMLDivElement>()
const costChartMode = ref<'bar' | 'area'>('bar')
const costChartModes = [{ label: '柱状图', value: 'bar' }, { label: '面积图', value: 'area' }]
let chart: EChartsType | undefined
let costChart: EChartsType | undefined

const success = computed(() => successRate(dashboard.value.summary.requests, dashboard.value.summary.successes))

async function load() {
  loading.value = true
  try {
    await Promise.all([
      store.loadChannels(),
      apiGet<Dashboard>('/dashboard', { days: days.value }).then((value) => { dashboard.value = value }),
    ])
    await nextTick()
    renderChart()
    renderCostChart()
  } catch (error) {
    showError(error, '加载仪表盘失败')
  } finally {
    loading.value = false
  }
}

function renderChart() {
  if (!chartElement.value) return
  chart ||= init(chartElement.value)
  chart.setOption({
    animationDuration: 450,
    color: ['#1677ff', '#16866f'],
    grid: { top: 24, right: 18, bottom: 28, left: 48 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, right: 8, textStyle: { color: '#66717d', fontSize: 11 } },
    xAxis: { type: 'category', data: dashboard.value.trend.map((item) => item.bucket.slice(5)), axisLine: { lineStyle: { color: '#dce2e7' } }, axisLabel: { color: '#66717d' } },
    yAxis: [
      { type: 'value', name: '请求', nameTextStyle: { color: '#66717d' }, splitLine: { lineStyle: { color: '#edf0f2' } } },
      { type: 'value', name: 'Token', nameTextStyle: { color: '#66717d' }, splitLine: { show: false } },
    ],
    series: [
      { name: '请求', type: 'bar', barMaxWidth: 22, data: dashboard.value.trend.map((item) => item.requests), itemStyle: { borderRadius: [3, 3, 0, 0] } },
      { name: 'Token', type: 'line', yAxisIndex: 1, smooth: true, symbolSize: 6, data: dashboard.value.trend.map((item) => item.inputTokens + item.outputTokens) },
    ],
  })
}

function renderCostChart() {
  if (!costChartElement.value || !dashboard.value.recentCost.models.length) return
  costChart ||= init(costChartElement.value)
  const models = dashboard.value.recentCost.models
  const buckets = models[0]?.points.map((point) => point.bucket.slice(5, 16)) || []
  const area = costChartMode.value === 'area'
  costChart.setOption({
    animationDuration: 450,
    color: ['#31bd7b', '#19a8d8', '#2563eb', '#f58a17', '#7c4dd8', '#8a94a6'],
    grid: { top: 28, right: 22, bottom: 56, left: 58 },
    tooltip: {
      trigger: 'axis',
      valueFormatter: (value: number | string) => formatCost(Number(value)),
    },
    legend: { bottom: 4, type: 'scroll', textStyle: { color: '#66717d', fontSize: 11 } },
    xAxis: {
      type: 'category',
      data: buckets,
      axisLine: { lineStyle: { color: '#dce2e7' } },
      axisLabel: { color: '#7b8792', hideOverlap: true, interval: 2 },
      axisTick: { show: false },
    },
    yAxis: {
      type: 'value',
      name: 'USD',
      nameTextStyle: { color: '#7b8792', padding: [0, 0, 0, -28] },
      axisLabel: { color: '#7b8792' },
      splitLine: { lineStyle: { color: '#edf0f2' } },
    },
    series: models.map((model) => ({
      name: model.name,
      type: area ? 'line' : 'bar',
      stack: 'cost',
      data: model.points.map((point) => point.estimatedCost),
      barMaxWidth: 34,
      smooth: area,
      showSymbol: false,
      areaStyle: area ? { opacity: 0.72 } : undefined,
      lineStyle: area ? { width: 1.5 } : undefined,
      emphasis: { focus: 'series' },
    })),
  }, true)
}

function resize() { chart?.resize(); costChart?.resize() }
watch(days, load)
watch(costChartMode, renderCostChart)
onMounted(() => { load(); window.addEventListener('resize', resize) })
onBeforeUnmount(() => { window.removeEventListener('resize', resize); chart?.dispose(); costChart?.dispose() })
</script>

<template>
  <div v-loading="loading" class="page-stack">
    <div class="page-toolbar">
      <el-segmented v-model="days" :options="[{ label: '7 天', value: 7 }, { label: '30 天', value: 30 }, { label: '90 天', value: 90 }]" />
      <div class="spacer" />
      <el-button :icon="RefreshCw" @click="load">刷新</el-button>
    </div>

    <section class="metric-grid" aria-label="核心指标">
      <article class="metric-card"><div class="label"><Activity :size="15" />总请求</div><div class="value">{{ formatNumber(dashboard.summary.requests) }}</div><div class="detail">成功率 {{ success }}</div></article>
      <article class="metric-card"><div class="label"><Route :size="15" />总 Token</div><div class="value">{{ formatNumber(dashboard.summary.totalTokens) }}</div><div class="detail">输入 {{ formatNumber(dashboard.summary.inputTokens) }} · 输出 {{ formatNumber(dashboard.summary.outputTokens) }}</div></article>
      <article class="metric-card"><div class="label"><CircleDollarSign :size="15" />估算成本</div><div class="value">{{ formatCost(dashboard.summary.estimatedCost) }}</div><div class="detail">未定价请求不计入</div></article>
      <article class="metric-card"><div class="label"><Clock3 :size="15" />平均耗时</div><div class="value">{{ Math.round(dashboard.summary.averageLatency) }} ms</div><div class="detail">包含上游响应时间</div></article>
    </section>

    <section class="recent-cost-panel panel" aria-label="近 24 小时消费分布">
      <div class="recent-cost-header">
        <div class="recent-cost-title">
          <span class="recent-cost-icon"><CircleDollarSign :size="17" /></span>
          <div><h2>消费分布</h2><span>近 24 小时 · 总计 {{ formatCost(dashboard.recentCost.totalEstimatedCost) }}</span></div>
        </div>
        <el-segmented v-model="costChartMode" :options="costChartModes" size="small" aria-label="消费图表类型" />
      </div>
      <div v-if="dashboard.recentCost.models.length" ref="costChartElement" class="recent-cost-chart" />
      <div v-else class="recent-cost-empty">近 24 小时暂无消费数据</div>
    </section>

    <section>
      <div class="section-heading"><h2>航线状态</h2><span>{{ store.channels.filter((item) => item.status === 1).length }} 条可用渠道</span></div>
      <RouteStrip :channels="store.channels" />
    </section>

    <section class="dashboard-grid panel">
      <div class="chart-block">
        <div class="section-heading"><h2>请求与 Token 趋势</h2><span>按天聚合</span></div>
        <div ref="chartElement" class="trend-chart" />
      </div>
      <div class="ranking-block">
        <div class="section-heading"><h2>模型用量</h2><span>前 8 项</span></div>
        <div class="ranking-list">
          <div v-for="item in dashboard.byModel" :key="item.name" class="ranking-row"><span>{{ item.name }}</span><strong>{{ formatNumber(item.totalTokens) }}</strong></div>
          <div v-if="!dashboard.byModel.length" class="empty-mini">暂无用量</div>
        </div>
      </div>
      <div class="ranking-block">
        <div class="section-heading"><h2>渠道请求</h2><span>前 8 项</span></div>
        <div class="ranking-list">
          <div v-for="item in dashboard.byChannel" :key="item.name" class="ranking-row"><span>{{ item.name }}</span><strong>{{ formatNumber(item.requests) }}</strong></div>
          <div v-if="!dashboard.byChannel.length" class="empty-mini">暂无用量</div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.recent-cost-panel { overflow: hidden; padding: 0; }
.recent-cost-header { display: flex; min-height: 54px; align-items: center; justify-content: space-between; gap: 16px; padding: 9px 14px; border-bottom: 1px solid #edf0f2; }
.recent-cost-title { display: flex; min-width: 0; align-items: center; gap: 10px; }
.recent-cost-title h2 { margin: 0; color: #15202b; font-size: 14px; font-weight: 700; }
.recent-cost-title span { color: #66717d; font-size: 11px; }
.recent-cost-icon { display: grid; width: 30px; height: 30px; flex: 0 0 30px; place-items: center; border-radius: 50%; color: #16866f !important; background: #e7f5f1; }
.recent-cost-chart, .recent-cost-empty { width: 100%; height: 330px; }
.recent-cost-empty { display: grid; place-items: center; color: #7b8792; font-size: 12px; }
.dashboard-grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(220px, 1fr) minmax(220px, 1fr); gap: 20px; }
.trend-chart { width: 100%; height: 300px; }
.ranking-list { border: 1px solid #dce2e7; border-radius: 6px; background: #fff; }
.ranking-row { display: flex; min-height: 39px; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 11px; border-bottom: 1px solid #edf0f2; font-size: 12px; }
.ranking-row:last-child { border-bottom: 0; }.ranking-row span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.ranking-row strong { font-family: 'JetBrains Mono', monospace; font-size: 11px; }.empty-mini { display: grid; min-height: 120px; place-items: center; color: #7b8792; font-size: 12px; }
@media (max-width: 1100px) { .dashboard-grid { grid-template-columns: 1fr 1fr; }.chart-block { grid-column: 1 / -1; } }
@media (max-width: 650px) { .recent-cost-header { align-items: stretch; flex-direction: column; }.recent-cost-chart, .recent-cost-empty { height: 280px; }.dashboard-grid { grid-template-columns: 1fr; }.chart-block { grid-column: auto; }.trend-chart { height: 250px; } }
</style>
