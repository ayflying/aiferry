<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RefreshCw, Search } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { ManagedUser, UsageLog, UsagePage } from '../api/types'
import UsageDetailDialog from '../components/UsageDetailDialog.vue'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { formatCost, formatLatency, formatNumber, formatStreamSpeed, formatTime } from '../lib/format'

const store = useAppStore()
const auth = useAuthStore()
const loading = ref(false)
const page = ref<UsagePage>({ items: [], total: 0, page: 1, pageSize: 20 })
const filters = reactive({ model: '', userId: undefined as number | undefined, channelId: undefined as number | undefined, apiKeyId: undefined as number | undefined, page: 1, pageSize: 20 })
const users = ref<ManagedUser[]>([])
const isAdmin = computed(() => auth.user?.isAdmin === true)
const usageItems = computed(() => page.value.items ?? [])
const selectedUsage = ref<UsageLog>()
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
function isSuccessful(row: UsageLog) { return row.httpStatus >= 200 && row.httpStatus < 300 }
function openUsageDetail(row: UsageLog) { selectedUsage.value = row; detailOpen.value = true }
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="toolbar-group">
        <el-input v-model="filters.model" clearable placeholder="模型名称" style="width: 200px" @keyup.enter="search" />
        <el-select v-if="isAdmin" v-model="filters.userId" clearable filterable placeholder="全部用户" style="width: 160px"><el-option v-for="item in users" :key="item.id" :label="item.nickname" :value="item.id" /></el-select>
        <el-select v-if="isAdmin" v-model="filters.channelId" clearable placeholder="全部渠道" style="width: 160px"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-select v-model="filters.apiKeyId" clearable placeholder="全部密钥" style="width: 160px"><el-option v-for="item in store.apiKeys" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-button type="primary" :icon="Search" @click="search">查询</el-button>
      </div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="usageItems" row-key="id">
        <el-table-column label="时间" min-width="156"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="模型" min-width="160"><template #default="{ row }"><div class="request-cell"><strong>{{ row.requestedModel }}</strong><small v-if="row.upstreamModel && row.upstreamModel !== row.requestedModel">→ {{ row.upstreamModel }}</small></div></template></el-table-column>
        <el-table-column v-if="isAdmin" label="用户" min-width="130"><template #default="{ row }">{{ row.userName || `#${row.userId}` }}</template></el-table-column>
        <el-table-column :label="isAdmin ? '渠道 / 密钥' : '访问密钥'" min-width="150"><template #default="{ row }"><div class="request-cell"><span v-if="isAdmin">{{ row.channelName || '—' }}</span><span v-else>{{ row.apiKeyName || '—' }}</span><small v-if="isAdmin">{{ row.apiKeyName || '—' }}</small></div></template></el-table-column>
        <el-table-column label="状态" width="86"><template #default="{ row }"><el-tag :type="isSuccessful(row) ? 'success' : 'danger'" effect="plain" size="small">{{ row.httpStatus }}</el-tag></template></el-table-column>
        <el-table-column label="流式" min-width="96"><template #default="{ row }"><div class="stream-cell"><strong>{{ row.isStream ? '流式' : '非流式' }}</strong><small>{{ row.isStream ? formatStreamSpeed(row.outputTokens, row.durationMs, row.firstTokenMs) : '—' }}</small></div></template></el-table-column>
        <el-table-column label="Token" min-width="150"><template #default="{ row }"><div class="token-cell"><strong>{{ formatNumber(row.totalTokens) }}</strong><small>入 {{ formatNumber(row.inputTokens) }} · 出 {{ formatNumber(row.outputTokens) }}</small></div></template></el-table-column>
        <el-table-column label="估算成本" min-width="125"><template #default="{ row }"><span :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</span></template></el-table-column>
        <el-table-column label="性能" min-width="164"><template #default="{ row }"><div class="performance-cell"><template v-if="row.isStream"><strong>首 token {{ formatLatency(row.firstTokenMs) }}</strong><small>总耗时 {{ formatLatency(row.durationMs) }}</small></template><strong v-else>总耗时 {{ formatLatency(row.durationMs) }}</strong></div></template></el-table-column>
        <el-table-column label="详情" min-width="150" show-overflow-tooltip><template #default="{ row }"><el-button v-if="isSuccessful(row)" text class="detail-trigger" title="查看请求详情" @click="openUsageDetail(row)"><span :class="row.estimatedCost == null ? 'muted' : 'mono'">{{ formatCost(row.estimatedCost) }}</span></el-button><span v-else class="danger-text">{{ row.errorMessage || '—' }}</span></template></el-table-column>
      </el-table>
      <div v-if="!loading && !usageItems.length" class="empty-state"><div><strong>暂无用量记录</strong><span>成功调用中转接口后会显示在这里</span></div></div>
      <div class="pagination-row"><el-pagination :current-page="filters.page" :page-size="filters.pageSize" :page-sizes="[20, 50, 100]" :total="page.total" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="changePageSize" /></div>
    </div>
    <UsageDetailDialog v-model="detailOpen" :usage="selectedUsage" />
  </div>
</template>

<style scoped>
.request-cell, .token-cell, .stream-cell, .performance-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.request-cell > span, .request-cell > strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.request-cell small, .token-cell small, .stream-cell small, .performance-cell small { color: #7b8792; font-size: 10px; }.token-cell strong, .stream-cell strong, .performance-cell strong, .mono { font-family: 'JetBrains Mono', monospace; font-size: 11px; }.detail-trigger { height: auto; padding: 0; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.detail-trigger:hover span:not(.muted) { text-decoration: underline; }
</style>
