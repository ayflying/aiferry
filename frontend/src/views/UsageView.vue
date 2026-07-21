<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RefreshCw, Search } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { BillingItem, ManagedUser, UsageLog, UsagePage } from '../api/types'
import UsageDetailDialog from '../components/UsageDetailDialog.vue'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { currentTimeInDisplayZone, formatCost, formatNumber, formatPreciseCost, formatReasoningEffort, formatTokenSpeed, formatTime } from '../lib/format'
import { formatIPLocation } from '../lib/ip-location'

const store = useAppStore()
const auth = useAuthStore()
const loading = ref(false)
const timeRange = ref(todayRange())
const page = ref<UsagePage>({ items: [], summary: { requests: 0, estimatedCost: 0 }, startAt: timeRange.value[0].toISOString(), endAt: endOfSecond(timeRange.value[1]).toISOString(), total: 0, page: 1, pageSize: 20 })
const filters = reactive({ model: '', userId: undefined as number | undefined, channelId: undefined as number | undefined, apiKeyId: undefined as number | undefined, startAt: timeRange.value[0].toISOString(), endAt: endOfSecond(timeRange.value[1]).toISOString(), page: 1, pageSize: 20 })
const users = ref<ManagedUser[]>([])
const isAdmin = computed(() => auth.user?.isAdmin === true)
const usageItems = computed(() => page.value.items ?? [])
const selectedUsage = ref<UsageLog>()
const detailOpen = ref(false)

async function load() {
  loading.value = true
  try {
    const dataPromise = apiGet<UsagePage>('/usage', filters)
    const support = [
      store.apiKeys.length ? Promise.resolve() : store.loadAPIKeys(),
    ]
    if (isAdmin.value) {
      support.push(store.channels.length ? Promise.resolve() : store.loadChannels())
      support.push(apiGet<ManagedUser[]>('/users').then((items) => { users.value = items }))
    }
    await Promise.all([dataPromise, ...support])
    page.value = await dataPromise
  } catch (error) { showError(error, '加载用量记录失败') } finally { loading.value = false }
}

function search() { filters.page = 1; load() }
function changePage(value: number) { filters.page = value; load() }
function changePageSize(value: number) { filters.pageSize = value; filters.page = 1; load() }
function todayRange(): [Date, Date] {
  const now = currentTimeInDisplayZone()
  return [now.startOf('day').toDate(), now.endOf('day').millisecond(0).toDate()]
}
function endOfSecond(value: Date) { return new Date(value.getTime() + 999) }
function changeTimeRange(value: [Date, Date] | null) {
  const next = value ?? todayRange()
  timeRange.value = next
  filters.startAt = next[0].toISOString()
  filters.endAt = endOfSecond(next[1]).toISOString()
  search()
}
function isSuccessful(row: UsageLog) { return row.httpStatus >= 200 && row.httpStatus < 300 }
function failurePreview(row: UsageLog) { return row.errorMessage.split('\n', 1)[0] || '查看失败日志' }
function latencyTone(value: number | undefined | null, fastMs: number, slowMs: number) {
  if (value === undefined || value === null) return 'latency-unknown'
  if (value <= fastMs) return 'latency-fast'
  if (value >= slowMs) return 'latency-slow'
  return 'latency-medium'
}
function firstTokenTone(row: UsageLog) { return latencyTone(row.firstTokenMs, 3_000, 10_000) }
function totalLatencyTone(row: UsageLog) { return latencyTone(row.durationMs, 5_000, 15_000) }
function formatElapsed(value?: number | null) {
  if (value === undefined || value === null) return '—'
  return `${(value / 1000).toFixed(1)}s`
}
function modelPriceSummary(row: UsageLog) {
  const details = row.billingDetails
  if (!details) return '无价格快照'
  const items = new Map<BillingItem['type'], BillingItem>()
  for (const item of details.items) {
    if (item.unit !== 'settlement') items.set(item.type, item)
  }
  const label = details.rule?.name || '标准'
  const input = items.get('input')
  const output = items.get('output')
  const primary = [input, output].filter((item): item is BillingItem => Boolean(item))
  if (!primary.length) {
    const request = items.get('request')
    return request ? `${label} · ${formatPreciseCost(request.unitPrice, details.currency)}/次` : `${label} · 未配置单价`
  }
  const extraCount = [...items.values()].filter((item) => item.type !== 'input' && item.type !== 'output').length
  const prices = primary.map((item) => formatPreciseCost(item.unitPrice, details.currency)).join(' / ')
  return `${label} · ${prices}/M${extraCount ? ` +${extraCount}` : ''}`
}
function openUsageDetail(row: UsageLog) {
  selectedUsage.value = row
  detailOpen.value = true
}
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="toolbar-group">
        <el-date-picker v-model="timeRange" type="datetimerange" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" :clearable="false" :editable="false" style="width: min(100%, 352px)" @change="changeTimeRange" />
        <el-input v-model="filters.model" clearable placeholder="模型名称" style="width: 200px" @keyup.enter="search" />
        <el-select v-if="isAdmin" v-model="filters.userId" clearable filterable placeholder="全部用户" style="width: 160px"><el-option v-for="item in users" :key="item.id" :label="item.nickname" :value="item.id" /></el-select>
        <el-select v-if="isAdmin" v-model="filters.channelId" clearable placeholder="全部渠道" style="width: 160px"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-select v-model="filters.apiKeyId" clearable placeholder="全部密钥" style="width: 160px"><el-option v-for="item in store.apiKeys" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-button type="primary" :icon="Search" @click="search">查询</el-button>
      </div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="usage-filter-summary" aria-live="polite">
      <span>费用</span>
      <strong class="mono">{{ formatCost(page.summary.estimatedCost) }}</strong>
      <i aria-hidden="true" />
      <span>请求</span>
      <strong class="mono">{{ formatNumber(page.summary.requests) }}</strong>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="usageItems" row-key="id">
        <el-table-column label="时间" min-width="230"><template #default="{ row }"><div class="time-cell"><strong>{{ formatTime(row.createdAt) }}</strong><small>{{ formatIPLocation(row.ipLocation) }}</small></div></template></el-table-column>
        <el-table-column label="模型" min-width="160"><template #default="{ row }"><div class="request-cell"><strong>{{ row.requestedModel }}</strong><small>推理强度：{{ formatReasoningEffort(row.reasoningEffort) }}</small></div></template></el-table-column>
        <el-table-column v-if="isAdmin" label="用户" min-width="130"><template #default="{ row }">{{ row.userName || `#${row.userId}` }}</template></el-table-column>
        <el-table-column :label="isAdmin ? '渠道 / 密钥' : '访问密钥'" min-width="150"><template #default="{ row }"><div class="request-cell"><span v-if="isAdmin">{{ row.channelName || '—' }}</span><span v-else>{{ row.apiKeyName || '—' }}</span><small v-if="isAdmin">{{ row.apiKeyName || '—' }}</small></div></template></el-table-column>
        <el-table-column label="状态" width="86"><template #default="{ row }"><el-tag :type="isSuccessful(row) ? 'success' : 'danger'" effect="plain" size="small">{{ row.httpStatus }}</el-tag></template></el-table-column>
        <el-table-column label="流式" min-width="96"><template #default="{ row }"><div class="stream-cell"><strong>{{ row.isStream ? '流式' : '非流式' }}</strong><small>{{ formatTokenSpeed(row.outputTokens, row.durationMs, row.firstTokenMs) }}</small></div></template></el-table-column>
        <el-table-column label="Token" min-width="185"><template #default="{ row }"><div class="token-cell"><strong>入 {{ formatNumber(row.inputTokens) }} · 出 {{ formatNumber(row.outputTokens) }}</strong><small>缓存 {{ formatNumber(row.cachedInputTokens) }}</small></div></template></el-table-column>
        <el-table-column label="估算成本" min-width="125"><template #default="{ row }"><span :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</span></template></el-table-column>
        <el-table-column label="耗时" min-width="116"><template #default="{ row }"><div class="latency-cell"><span class="latency-strip" :class="{ 'single-latency': !row.isStream }"><i :class="row.isStream ? firstTokenTone(row) : totalLatencyTone(row)" /><i v-if="row.isStream" :class="totalLatencyTone(row)" /></span><div class="latency-copy"><template v-if="row.isStream"><strong>首字 <span :class="firstTokenTone(row)">{{ formatElapsed(row.firstTokenMs) }}</span></strong><small>耗时 <span :class="totalLatencyTone(row)">{{ formatElapsed(row.durationMs) }}</span></small></template><template v-else><strong>响应 <span :class="totalLatencyTone(row)">{{ formatElapsed(row.durationMs) }}</span></strong><small>非流式响应</small></template></div></div></template></el-table-column>
        <el-table-column label="详情" min-width="190"><template #default="{ row }"><el-button text class="detail-trigger" @click="openUsageDetail(row)"><span v-if="isSuccessful(row)" class="price-cell"><strong>{{ modelPriceSummary(row) }}</strong></span><span v-else class="failure-cell"><strong>{{ failurePreview(row) }}</strong><small>查看失败日志</small></span></el-button></template></el-table-column>
      </el-table>
      <div v-if="!loading && !usageItems.length" class="empty-state"><div><strong>暂无用量记录</strong><span>成功调用中转接口后会显示在这里</span></div></div>
      <div class="pagination-row"><el-pagination :current-page="filters.page" :page-size="filters.pageSize" :page-sizes="[20, 50, 100]" :total="page.total" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="changePageSize" /></div>
    </div>
    <UsageDetailDialog v-model="detailOpen" :usage="selectedUsage" />
  </div>
</template>

<style scoped>
.time-cell, .request-cell, .token-cell, .stream-cell, .price-cell, .failure-cell { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.time-cell strong, .time-cell small, .request-cell > span, .request-cell > strong, .price-cell strong, .price-cell small, .failure-cell strong, .failure-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.time-cell small, .request-cell small, .token-cell small, .stream-cell small, .price-cell small, .failure-cell small { color: #7b8792; font-size: 11px; }.time-cell strong, .token-cell strong, .stream-cell strong, .price-cell strong, .failure-cell strong, .mono { font-family: 'JetBrains Mono', monospace; font-size: 12px; }.failure-cell strong { color: #d14343; }.latency-cell { display: flex; min-width: 0; align-items: stretch; gap: 8px; }.latency-strip { display: flex; width: 4px; min-width: 4px; flex-direction: column; overflow: hidden; }.latency-strip i { flex: 1; }.latency-strip.single-latency i { height: 100%; }.latency-copy { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: #4d5c69; font-family: 'JetBrains Mono', monospace; font-size: 13px; line-height: 1.2; }.latency-copy strong, .latency-copy small { font: inherit; white-space: nowrap; }.latency-copy small { font-size: 12px; }.latency-fast { color: #14956a; }.latency-medium { color: #cf8916; }.latency-slow { color: #ef3f4d; }.latency-unknown { color: #7b8792; }.latency-strip .latency-fast { background: #14956a; }.latency-strip .latency-medium { background: #cf8916; }.latency-strip .latency-slow { background: #ef3f4d; }.latency-strip .latency-unknown { background: #b7c1ca; }.detail-trigger { height: auto; max-width: 100%; padding: 0; text-align: left; }.detail-trigger:hover .price-cell strong, .detail-trigger:hover .failure-cell strong { text-decoration: underline; }
.usage-filter-summary { display: flex; align-items: center; gap: 8px; min-height: 34px; padding: 0 2px; color: #6c7a88; border-top: 1px solid #e6ebf0; border-bottom: 1px solid #e6ebf0; font-size: 12px; }.usage-filter-summary strong { color: #293643; font-variant-numeric: tabular-nums; }.usage-filter-summary i { width: 1px; height: 14px; margin: 0 4px; background: #d9e1e8; }
</style>
