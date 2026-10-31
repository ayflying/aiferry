<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { FileText, RefreshCw, Search, TriangleAlert } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { APIKey, BillingItem, UsageLog, UsagePage, UserOption } from '../api/types'
import ChannelCredentialDisplay from '../components/ChannelCredentialDisplay.vue'
import UsageDetailDialog from '../components/UsageDetailDialog.vue'
import MobileRecordList from '../components/MobileRecordList.vue'
import ResponsiveList from '../components/ResponsiveList.vue'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { currentTimeInDisplayZone, formatCost, formatNumber, formatPreciseCost, formatReasoningEffort, formatTokenSpeed, formatTime, formatUsageDuration } from '../lib/format'
import { formatIPLocation } from '../lib/ip-location'
import { streamLabel } from '../lib/usage'
import { USAGE_COLUMN_WIDTHS, usageDetailHint, visibleUsageColumns, type UsageColumnKey } from '../lib/usage-columns'
import { useElementWidth } from '../composables/useElementWidth'

const props = defineProps<{ userId?: number }>()
const store = useAppStore()
const auth = useAuthStore()
const loading = ref(false)
const timeRange = ref(props.userId !== undefined ? recentRange(30) : todayRange())
const page = ref<UsagePage>({ items: [], summary: { requests: 0, estimatedCost: 0 }, startAt: timeRange.value[0].toISOString(), endAt: endOfSecond(timeRange.value[1]).toISOString(), total: 0, page: 1, pageSize: 20 })
const filters = reactive({ model: '', userId: undefined as number | undefined, channelId: undefined as number | undefined, apiKeyId: undefined as number | undefined, startAt: timeRange.value[0].toISOString(), endAt: endOfSecond(timeRange.value[1]).toISOString(), page: 1, pageSize: 20 })
const users = ref<UserOption[]>([])
const usersLoaded = ref(false)
const isAdmin = computed(() => auth.user?.isAdmin === true)
const selectedUserId = computed(() => props.userId ?? filters.userId ?? auth.user?.id)
const embeddedForUser = computed(() => props.userId !== undefined)
const usageItems = computed(() => page.value.items ?? [])
const selectedUsage = ref<UsageLog>()
const detailOpen = ref(false)
// 表格可用宽度按面板实测：1280/1366 这类笔记本屏幕放不下全部列，
// 由 visibleUsageColumns 决定加不加「用户」「流式」列，避免整页右侧超框。
const tablePanel = ref<HTMLElement>()
const tablePanelWidth = useElementWidth(tablePanel)
const visibleColumns = computed(() => visibleUsageColumns(Math.max(tablePanelWidth.value - 2, 0), { isAdmin: isAdmin.value }))
// 列集合变化时重建表格，避免 ElTable 复用旧的固定列 DOM 导致列宽错位。
const columnSignature = computed(() => [...visibleColumns.value].sort().join('-'))
// 必显列都放不下时表格需要内部横向滚动：此时常驻显示横向滚动条，用户才知道右边还有内容。
const columnsOverflow = computed(() => tablePanelWidth.value > 0
  && [...visibleColumns.value].reduce((sum, key) => sum + USAGE_COLUMN_WIDTHS[key], 0) > tablePanelWidth.value - 2)
function hasColumn(key: UsageColumnKey) { return visibleColumns.value.has(key) }
function columnMinWidth(key: UsageColumnKey) { return USAGE_COLUMN_WIDTHS[key] }

async function load() {
  loading.value = true
  try {
    await loadSupport()
    const query = { ...filters, userId: props.userId ?? filters.userId }
    page.value = await apiGet<UsagePage>('/usage', query)
  } catch (error) { showError(error, '加载用量记录失败') } finally { loading.value = false }
}

async function loadSupport() {
  const support: Promise<unknown>[] = []
  if (!isAdmin.value) {
    support.push(store.apiKeys.length ? Promise.resolve() : store.loadAPIKeys())
  }
  if (isAdmin.value) {
    support.push(store.channels.length ? Promise.resolve() : store.loadChannels())
    if (!embeddedForUser.value && !usersLoaded.value) {
      support.push(apiGet<UserOption[]>('/users', { compact: 1 }).then((items) => {
        users.value = items
        usersLoaded.value = true
      }))
    }
  }
  if (selectedUserId.value && (embeddedForUser.value || filters.userId !== undefined || isAdmin.value)) {
    support.push(apiGet<APIKey[]>(`/api-keys`, { userId: selectedUserId.value }).then((items) => {
      store.apiKeys = items ?? []
    }))
  }
  await Promise.all(support)
}

function search() { filters.page = 1; load() }

watch(() => filters.userId, () => {
  if (embeddedForUser.value) return
  filters.apiKeyId = undefined
  search()
})
watch(() => props.userId, (id) => {
  if (id === undefined) return
  filters.apiKeyId = undefined
  filters.userId = id
  search()
})
function changePage(value: number) { filters.page = value; load() }
function changePageSize(value: number) { filters.pageSize = value; filters.page = 1; load() }
function recentRange(days: number): [Date, Date] {
  const now = currentTimeInDisplayZone()
  return [now.startOf('day').subtract(days - 1, 'day').toDate(), now.endOf('day').millisecond(0).toDate()]
}
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
// cachedInputTokens 由上游 usage 返回：缺失表示上游没有提供缓存统计，0 表示明确未命中，正数表示命中缓存。
function cacheStatus(row: UsageLog) {
  if (row.cachedInputTokens === undefined || row.cachedInputTokens === null) return '上游未返回'
  if (row.cachedInputTokens === 0) return '未命中'
  return `命中 ${formatNumber(row.cachedInputTokens)}`
}
function failurePreview(row: UsageLog) { return row.errorMessage.split('\n', 1)[0] || '查看失败日志' }
function latencyTone(value: number | undefined | null, fastMs: number, slowMs: number) {
  if (value === undefined || value === null) return 'latency-unknown'
  if (value <= fastMs) return 'latency-fast'
  if (value >= slowMs) return 'latency-slow'
  return 'latency-medium'
}
function firstTokenTone(row: UsageLog) { return latencyTone(row.firstTokenMs, 3_000, 10_000) }
function totalLatencyTone(row: UsageLog) { return latencyTone(row.durationMs, 5_000, 15_000) }
// 模型健康评分分色：与渠道模型测试弹窗一致（≥80 绿、≥50 黄、<50 红）
function modelHealthTone(score: number) {
  if (score >= 80) return 'latency-fast'
  if (score >= 50) return 'latency-medium'
  return 'latency-slow'
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
onMounted(() => {
  if (props.userId !== undefined) filters.userId = props.userId
  else if (isAdmin.value && auth.user?.id) filters.userId = auth.user.id
  load()
})
</script>

<template>
  <div v-loading="loading" class="page-stack">
    <div class="page-toolbar usage-toolbar">
      <div class="toolbar-group usage-filters">
        <el-date-picker v-model="timeRange" class="filter-range" type="datetimerange" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" :clearable="false" :editable="false" @change="changeTimeRange" />
        <el-input v-model="filters.model" class="filter-input" clearable placeholder="模型名称" @keyup.enter="search" />
        <el-select v-if="isAdmin && !embeddedForUser" v-model="filters.userId" class="filter-select" filterable placeholder="选择用户"><el-option v-for="item in users" :key="item.id" :label="item.nickname" :value="item.id" /></el-select>
        <el-select v-if="isAdmin" v-model="filters.channelId" class="filter-select" clearable placeholder="全部渠道"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-select v-model="filters.apiKeyId" class="filter-select" clearable placeholder="全部密钥"><el-option v-for="item in store.apiKeys" :key="item.id" :label="item.name" :value="item.id" /></el-select>
      </div>
      <el-button class="toolbar-search" type="primary" :icon="Search" @click="search">查询</el-button>
      <div class="spacer" />
      <el-button class="toolbar-refresh" :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="usage-filter-summary" aria-live="polite">
      <span>费用</span>
      <strong class="mono">{{ formatCost(page.summary.estimatedCost) }}</strong>
      <i aria-hidden="true" />
      <span>请求</span>
      <strong class="mono">{{ formatNumber(page.summary.requests) }}</strong>
    </div>

    <div class="table-panel" ref="tablePanel">
      <ResponsiveList>
        <template #desktop><el-table :key="columnSignature" v-loading="loading" :data="usageItems" row-key="id" :scrollbar-always-on="columnsOverflow">
        <el-table-column label="时间" :min-width="columnMinWidth('time')" fixed="left"><template #default="{ row }"><div class="time-cell"><strong>{{ formatTime(row.createdAt) }}</strong><small>{{ formatIPLocation(row.ipLocation) }}</small></div></template></el-table-column>
        <el-table-column label="模型" :min-width="columnMinWidth('model')"><template #default="{ row }"><div class="request-cell"><strong>{{ row.requestedModel }}</strong><small>推理强度：{{ formatReasoningEffort(row.reasoningEffort) }}</small></div></template></el-table-column>
        <el-table-column v-if="hasColumn('user')" label="用户" :min-width="columnMinWidth('user')"><template #default="{ row }">{{ row.userName || `#${row.userId}` }}</template></el-table-column>
        <el-table-column :label="isAdmin ? '渠道 / 密钥' : '访问密钥'" :min-width="columnMinWidth('channel')"><template #default="{ row }"><ChannelCredentialDisplay v-if="isAdmin" class="request-cell" :channel-name="row.channelName" :credential-index="row.channelCredentialIndex" :api-key-name="row.apiKeyName" /><div v-else class="request-cell"><span>{{ row.apiKeyName || '—' }}</span></div></template></el-table-column>
        <el-table-column label="状态" :min-width="columnMinWidth('status')"><template #default="{ row }"><div class="status-cell"><el-tag :type="isSuccessful(row) ? 'success' : 'danger'" effect="plain" size="small">{{ row.httpStatus }}</el-tag><small v-if="row.modelHealthScore != null" :class="modelHealthTone(row.modelHealthScore)">健康分 {{ row.modelHealthScore }}</small><small v-else class="latency-unknown">健康分 —</small></div></template></el-table-column>
        <el-table-column v-if="hasColumn('stream')" label="流式" :min-width="columnMinWidth('stream')"><template #default="{ row }"><div class="stream-cell"><strong>{{ streamLabel(row) }}</strong><small>{{ formatTokenSpeed(row.outputTokens, row.durationMs) }}</small></div></template></el-table-column>
        <el-table-column label="Token" :min-width="columnMinWidth('tokens')"><template #default="{ row }"><div class="token-cell"><strong>入 {{ formatNumber(row.inputTokens) }} · 出 {{ formatNumber(row.outputTokens) }}</strong><small>缓存 {{ formatNumber(row.cachedInputTokens) }}</small></div></template></el-table-column>
        <el-table-column label="估算成本" :min-width="columnMinWidth('cost')"><template #default="{ row }"><div class="cost-cell"><strong :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</strong><small class="price-hint" :title="modelPriceSummary(row)">{{ modelPriceSummary(row) }}</small></div></template></el-table-column>
        <el-table-column label="耗时" :min-width="columnMinWidth('latency')"><template #default="{ row }"><div class="latency-cell"><span class="latency-strip" :class="{ 'single-latency': !row.isStream }"><i :class="row.isStream ? firstTokenTone(row) : totalLatencyTone(row)" /><i v-if="row.isStream" :class="totalLatencyTone(row)" /></span><div class="latency-copy"><template v-if="row.isStream"><strong>首字 <span :class="firstTokenTone(row)">{{ formatUsageDuration(row.firstTokenMs) }}</span></strong><small>耗时 <span :class="totalLatencyTone(row)">{{ formatUsageDuration(row.durationMs) }}</span></small></template><template v-else><strong>响应 <span :class="totalLatencyTone(row)">{{ formatUsageDuration(row.durationMs) }}</span></strong><small>非流式响应</small></template></div></div></template></el-table-column>
        <el-table-column label="详情" :width="columnMinWidth('detail')" fixed="right" align="center"><template #default="{ row }"><el-tooltip :content="usageDetailHint(row, modelPriceSummary(row), failurePreview(row))" placement="top" :show-after="200"><el-button text class="detail-trigger" :class="{ 'detail-trigger--failed': !isSuccessful(row) }" @click="openUsageDetail(row)"><span class="detail-link"><FileText v-if="isSuccessful(row)" :size="14" /><TriangleAlert v-else :size="14" />{{ isSuccessful(row) ? '计费' : '失败' }}</span></el-button></el-tooltip></template></el-table-column>
        </el-table></template>
        <template #mobile><MobileRecordList :loading="loading">
          <article v-for="row in usageItems" :key="row.id" class="mobile-record">
            <div class="mobile-record__header"><div class="mobile-record__title"><strong>{{ row.requestedModel }}</strong><small>{{ formatTime(row.createdAt) }} · {{ formatIPLocation(row.ipLocation) }}</small></div><div class="mobile-record__status"><el-tag :type="isSuccessful(row) ? 'success' : 'danger'" effect="plain" size="small">{{ row.httpStatus }}</el-tag><small v-if="row.modelHealthScore != null" :class="modelHealthTone(row.modelHealthScore)">健康分 {{ row.modelHealthScore }}</small><small v-else class="latency-unknown">健康分 —</small></div></div>
            <dl class="mobile-record__facts"><div><dt>{{ isAdmin ? '渠道 / 密钥' : '访问密钥' }}</dt><dd><ChannelCredentialDisplay v-if="isAdmin" class="request-cell" :channel-name="row.channelName" :credential-index="row.channelCredentialIndex" :api-key-name="row.apiKeyName" /><span v-else>{{ row.apiKeyName || '—' }}</span></dd></div><div v-if="isAdmin"><dt>用户</dt><dd>{{ row.userName || `#${row.userId}` }}</dd></div><div><dt>Token</dt><dd class="mono">入 {{ formatNumber(row.inputTokens) }} · 出 {{ formatNumber(row.outputTokens) }}</dd></div><div><dt>请求缓存</dt><dd class="mono" :class="{ muted: row.cachedInputTokens == null }">{{ cacheStatus(row) }}</dd></div><div><dt>估算成本</dt><dd :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</dd></div><div><dt>{{ row.isStream ? '流式速度' : '响应耗时' }}</dt><dd>{{ row.isStream ? formatTokenSpeed(row.outputTokens, row.durationMs) : formatUsageDuration(row.durationMs) }}</dd></div><div><dt>推理强度</dt><dd>{{ formatReasoningEffort(row.reasoningEffort) }}</dd></div></dl>
            <div class="mobile-record__footer"><span class="muted">{{ streamLabel(row, true) }}</span><el-button size="small" @click="openUsageDetail(row)">{{ isSuccessful(row) ? '查看计费详情' : '查看失败日志' }}</el-button></div>
          </article>
        </MobileRecordList></template>
      </ResponsiveList>
      <div v-if="!loading && !usageItems.length" class="empty-state"><div><strong>暂无用量记录</strong><span>成功调用中转接口后会显示在这里</span></div></div>
      <div class="pagination-row"><el-pagination :current-page="filters.page" :page-size="filters.pageSize" :page-sizes="[20, 50, 100]" :total="page.total" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="changePageSize" /></div>
    </div>
    <UsageDetailDialog v-model="detailOpen" :usage="selectedUsage" />
  </div>
</template>

<style scoped>
/* 单元格统一两行堆叠：主信息 + 次要信息，长文本省略，避免撑宽列宽导致整表超框 */
.time-cell, .request-cell, .token-cell, .stream-cell, .cost-cell { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.time-cell strong, .time-cell small, .request-cell > span, .request-cell > strong, .cost-cell strong, .cost-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.time-cell small, .request-cell small, .token-cell small, .stream-cell small { color: #7b8792; font-size: 11px; }
.time-cell strong, .token-cell strong, .stream-cell strong, .cost-cell strong, .mono { font-family: 'JetBrains Mono', monospace; font-size: 12px; }
.price-hint { overflow: hidden; color: #7b8792; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.latency-cell { display: flex; min-width: 0; align-items: stretch; gap: 8px; }
.latency-strip { display: flex; width: 4px; min-width: 4px; flex-direction: column; overflow: hidden; }
.latency-strip i { flex: 1; }
.latency-strip.single-latency i { height: 100%; }
.latency-copy { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: #4d5c69; font-family: 'JetBrains Mono', monospace; font-size: 13px; line-height: 1.2; }
.latency-copy strong, .latency-copy small { font: inherit; white-space: nowrap; }
.latency-copy small { font-size: 12px; }
.latency-fast { color: #14956a; }
.latency-medium { color: #cf8916; }
.latency-slow { color: #ef3f4d; }
.latency-unknown { color: #7b8792; }
.latency-strip .latency-fast { background: #14956a; }
.latency-strip .latency-medium { background: #cf8916; }
.latency-strip .latency-slow { background: #ef3f4d; }
.latency-strip .latency-unknown { background: #b7c1ca; }
/* 详情列固定在表格右侧：横向滚动时「计费/失败」入口始终可见可点 */
.detail-trigger { height: auto; padding: 0; }
.detail-link { display: inline-flex; align-items: center; gap: 4px; color: #1677ff; font-size: 12px; white-space: nowrap; }
.detail-trigger--failed .detail-link { color: #d14343; }
.detail-trigger:hover .detail-link { text-decoration: underline; }
/* 筛选项按容器弹性伸缩：窄屏自动换行，不再把「查询/刷新」挤出视口 */
.usage-toolbar { row-gap: 8px; }
.usage-toolbar .usage-filters { flex: 1 1 auto; }
.usage-toolbar .toolbar-search, .usage-toolbar .toolbar-refresh { flex: 0 0 auto; margin: 0; }
.usage-toolbar .filter-range { flex: 1 1 300px; min-width: 236px; max-width: 352px; }
.usage-toolbar .filter-input { flex: 1 1 150px; min-width: 118px; max-width: 200px; }
.usage-toolbar .filter-select { flex: 1 1 132px; min-width: 110px; max-width: 160px; }
.usage-filter-summary { display: flex; align-items: center; gap: 8px; min-height: 34px; padding: 0 2px; color: #6c7a88; border-top: 1px solid #e6ebf0; border-bottom: 1px solid #e6ebf0; font-size: 12px; }
.usage-filter-summary strong { color: #293643; font-variant-numeric: tabular-nums; }
.usage-filter-summary i { width: 1px; height: 14px; margin: 0 4px; background: #d9e1e8; }
.status-cell { display: flex; flex-direction: column; align-items: center; gap: 3px; }
.status-cell small { font-size: 11px; line-height: 1.2; }
.mobile-record__status { display: flex; flex-direction: column; align-items: flex-end; gap: 3px; white-space: nowrap; }
.mobile-record__status small { font-size: 11px; line-height: 1.2; }
</style>
