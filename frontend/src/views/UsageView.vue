<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, reactive, ref } from 'vue'
import { RefreshCw, Search } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { ManagedUser, PriceRule, PublicModel, UsageLog, UsagePage } from '../api/types'
import UsageDetailDialog from '../components/UsageDetailDialog.vue'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { formatCost, formatLatency, formatNumber, formatStreamSpeed, formatTime } from '../lib/format'

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
const selectedModel = ref<PublicModel>()
const selectedPriceRules = ref<PriceRule[]>([])
const priceLoading = ref(false)
const detailOpen = ref(false)

async function load() {
  loading.value = true
  try {
    const dataPromise = apiGet<UsagePage>('/usage', filters)
    const support = [store.apiKeys.length ? Promise.resolve() : store.loadAPIKeys()]
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
  const now = dayjs()
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
function protocolName(endpoint: string) {
  if (endpoint === '/chat/completions') return 'Chat'
  if (endpoint === '/responses') return 'Responses'
  return endpoint || '—'
}
function protocolRoute(row: UsageLog) {
  const upstream = row.upstreamEndpoint || row.endpoint
  return row.protocolConversion ? `${protocolName(row.endpoint)} → ${protocolName(upstream)}` : protocolName(row.endpoint)
}
async function openUsageDetail(row: UsageLog) {
  selectedUsage.value = row
  selectedModel.value = undefined
  selectedPriceRules.value = []
  detailOpen.value = true
  priceLoading.value = true
  try {
    const models = await apiGet<PublicModel[]>('/public-models')
    const model = models.find((item) => item.publicName === row.requestedModel)
    if (!model || selectedUsage.value?.requestId !== row.requestId) return
    selectedModel.value = model
    if (model.billingMode === 'rules') {
      const rules = await apiGet<PriceRule[]>(`/models/${model.id}/price-rules`)
      if (selectedUsage.value?.requestId === row.requestId) selectedPriceRules.value = rules
    }
  } catch (error) {
    showError(error, '加载模型价格失败')
  } finally {
    if (selectedUsage.value?.requestId === row.requestId) priceLoading.value = false
  }
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
      <span>筛选时段</span>
      <time>{{ formatTime(page.startAt) }}</time>
      <span>至</span>
      <time>{{ formatTime(page.endAt) }}</time>
      <i aria-hidden="true" />
      <span>费用</span>
      <strong class="mono">{{ formatCost(page.summary.estimatedCost) }}</strong>
      <i aria-hidden="true" />
      <span>请求</span>
      <strong class="mono">{{ formatNumber(page.summary.requests) }}</strong>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="usageItems" row-key="id">
        <el-table-column label="时间" min-width="156"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="模型" min-width="160"><template #default="{ row }"><div class="request-cell"><strong>{{ row.requestedModel }}</strong><small v-if="row.upstreamModel && row.upstreamModel !== row.requestedModel">→ {{ row.upstreamModel }}</small></div></template></el-table-column>
        <el-table-column v-if="isAdmin" label="用户" min-width="130"><template #default="{ row }">{{ row.userName || `#${row.userId}` }}</template></el-table-column>
        <el-table-column :label="isAdmin ? '渠道 / 密钥' : '访问密钥'" min-width="150"><template #default="{ row }"><div class="request-cell"><span v-if="isAdmin">{{ row.channelName || '—' }}</span><span v-else>{{ row.apiKeyName || '—' }}</span><small v-if="isAdmin">{{ row.apiKeyName || '—' }}</small></div></template></el-table-column>
        <el-table-column label="协议" min-width="128"><template #default="{ row }"><div class="protocol-cell" :class="{ converted: Boolean(row.protocolConversion) }"><strong>{{ protocolRoute(row) }}</strong><small>{{ row.protocolConversion ? '已智能转换' : '原始协议' }}</small></div></template></el-table-column>
        <el-table-column label="状态" width="86"><template #default="{ row }"><el-tag :type="isSuccessful(row) ? 'success' : 'danger'" effect="plain" size="small">{{ row.httpStatus }}</el-tag></template></el-table-column>
        <el-table-column label="流式" min-width="96"><template #default="{ row }"><div class="stream-cell"><strong>{{ row.isStream ? '流式' : '非流式' }}</strong><small>{{ row.isStream ? formatStreamSpeed(row.outputTokens, row.durationMs, row.firstTokenMs) : '—' }}</small></div></template></el-table-column>
        <el-table-column label="Token" min-width="185"><template #default="{ row }"><div class="token-cell"><strong>{{ formatNumber(row.totalTokens) }}</strong><small>入 {{ formatNumber(row.inputTokens) }} · 缓存 {{ formatNumber(row.cachedInputTokens) }} · 出 {{ formatNumber(row.outputTokens) }}</small></div></template></el-table-column>
        <el-table-column label="估算成本" min-width="125"><template #default="{ row }"><span :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</span></template></el-table-column>
        <el-table-column label="性能" min-width="164"><template #default="{ row }"><div class="performance-cell"><template v-if="row.isStream"><strong>首 token {{ formatLatency(row.firstTokenMs) }}</strong><small>总耗时 {{ formatLatency(row.durationMs) }}</small></template><strong v-else>总耗时 {{ formatLatency(row.durationMs) }}</strong></div></template></el-table-column>
        <el-table-column label="详情" min-width="150" show-overflow-tooltip><template #default="{ row }"><el-button v-if="isSuccessful(row)" text class="detail-trigger" title="查看请求详情" @click="openUsageDetail(row)"><span :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</span></el-button><span v-else class="danger-text">{{ row.errorMessage || '—' }}</span></template></el-table-column>
      </el-table>
      <div v-if="!loading && !usageItems.length" class="empty-state"><div><strong>暂无用量记录</strong><span>成功调用中转接口后会显示在这里</span></div></div>
      <div class="pagination-row"><el-pagination :current-page="filters.page" :page-size="filters.pageSize" :page-sizes="[20, 50, 100]" :total="page.total" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="changePageSize" /></div>
    </div>
    <UsageDetailDialog v-model="detailOpen" :usage="selectedUsage" :model="selectedModel" :price-rules="selectedPriceRules" :price-loading="priceLoading" />
  </div>
</template>

<style scoped>
.request-cell, .token-cell, .stream-cell, .performance-cell, .protocol-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.request-cell > span, .request-cell > strong, .protocol-cell strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.request-cell small, .token-cell small, .stream-cell small, .performance-cell small, .protocol-cell small { color: #7b8792; font-size: 10px; }.protocol-cell.converted strong { color: #1677ff; }.token-cell strong, .stream-cell strong, .performance-cell strong, .protocol-cell strong, .mono { font-family: 'JetBrains Mono', monospace; font-size: 11px; }.detail-trigger { height: auto; padding: 0; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.detail-trigger:hover span:not(.muted) { text-decoration: underline; }
.usage-filter-summary { display: flex; align-items: center; gap: 8px; min-height: 34px; padding: 0 2px; color: #6c7a88; border-top: 1px solid #e6ebf0; border-bottom: 1px solid #e6ebf0; font-size: 12px; }.usage-filter-summary time, .usage-filter-summary strong { color: #293643; font-variant-numeric: tabular-nums; }.usage-filter-summary i { width: 1px; height: 14px; margin: 0 4px; background: #d9e1e8; }
@media (max-width: 720px) { .usage-filter-summary { align-items: flex-start; flex-wrap: wrap; gap: 5px 7px; padding: 7px 2px; }.usage-filter-summary i { display: none; }.usage-filter-summary time { white-space: nowrap; } }
</style>
