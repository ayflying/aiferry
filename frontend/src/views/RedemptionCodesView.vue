<script setup lang="ts">
import dayjs from 'dayjs'
import { onMounted, reactive, ref } from 'vue'
import { Copy, Plus, Search, Ticket, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost } from '../api/client'
import type { CreatedRedemptionCode, RedemptionCode, RedemptionCodeStatus } from '../api/types'
import TableActionButton from '../components/TableActionButton.vue'
import MobileRecordList from '../components/MobileRecordList.vue'
import ResponsiveList from '../components/ResponsiveList.vue'
import { copyText } from '../lib/clipboard'
import { showError } from '../lib/error'
import { formatCost, formatTime } from '../lib/format'

type StatusFilter = RedemptionCodeStatus | 'all'
type ExpiryPreset = 'never' | 'month' | 'week' | 'day'

const loading = ref(false)
const saving = ref(false)
const deleting = ref(false)
const dialogOpen = ref(false)
const codes = ref<RedemptionCode[]>([])
const createdCodes = ref<CreatedRedemptionCode[]>([])
const filters = reactive<{ keyword: string; status: StatusFilter }>({ keyword: '', status: 'all' })
const form = reactive<{ name: string; amount: number | undefined; expiry: ExpiryPreset; quantity: number }>({ name: '', amount: undefined, expiry: 'never', quantity: 1 })
const statusOptions: Array<{ label: string; value: StatusFilter }> = [
  { label: '全部状态', value: 'all' },
  { label: '可兑换', value: 'active' },
  { label: '已兑换', value: 'used' },
  { label: '已过期', value: 'expired' },
]

async function load() {
  loading.value = true
  try {
    codes.value = await apiGet<RedemptionCode[]>('/redemption-codes', { keyword: filters.keyword.trim(), status: filters.status })
  } catch (error) { showError(error, '加载兑换码失败') } finally { loading.value = false }
}

function openCreate() {
  Object.assign(form, { name: '', amount: undefined, expiry: 'never', quantity: 1 })
  createdCodes.value = []
  dialogOpen.value = true
}

function expiryTime() {
  if (form.expiry === 'month') return dayjs().add(1, 'month').toISOString()
  if (form.expiry === 'week') return dayjs().add(1, 'week').toISOString()
  if (form.expiry === 'day') return dayjs().add(1, 'day').toISOString()
  return undefined
}

async function create() {
  const name = form.name.trim()
  if (!name || [...name].length > 20) { showError('名称长度应为 1 到 20 个字符', '信息不完整'); return }
  if (!form.amount || form.amount <= 0) { showError('兑换额度必须大于 0', '信息不完整'); return }
  if (!Number.isInteger(form.quantity) || form.quantity < 1 || form.quantity > 100) { showError('批量数量应为 1 到 100', '信息不完整'); return }
  saving.value = true
  try {
    createdCodes.value = await apiPost<CreatedRedemptionCode[]>('/redemption-codes', {
      name,
      amount: form.amount,
      expiresAt: expiryTime(),
      quantity: form.quantity,
    })
    ElMessage.success(`已创建 ${createdCodes.value.length} 个兑换码`)
    await load()
  } catch (error) { showError(error, '创建兑换码失败') } finally { saving.value = false }
}

async function copyCode(code: string) {
  try { await copyText(code); ElMessage.success('兑换码已复制') } catch (error) { showError(error, '复制兑换码失败') }
}

async function removeInvalid() {
  try {
    await ElMessageBox.confirm('将永久删除所有已兑换和已过期的兑换码，无法恢复。', '删除无效兑换码', { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' })
    deleting.value = true
    const result = await apiDelete<{ deleted: number }>('/redemption-codes/invalid')
    result.deleted ? ElMessage.success(`已删除 ${result.deleted} 个无效兑换码`) : ElMessage.info('没有可删除的无效兑换码')
    await load()
  } catch (error) { if (error !== 'cancel') showError(error, '删除无效兑换码失败') } finally { deleting.value = false }
}

function statusLabel(status: RedemptionCodeStatus) {
  return status === 'active' ? '可兑换' : status === 'used' ? '已兑换' : '已过期'
}

function statusClass(status: RedemptionCodeStatus) {
  return status === 'active' ? 'success' : status === 'expired' ? 'warning' : ''
}

onMounted(load)
</script>

<template>
  <div class="page-stack redemption-page">
    <div class="page-toolbar redemption-toolbar">
      <div class="toolbar-group filter-group">
        <el-input v-model="filters.keyword" clearable :prefix-icon="Search" placeholder="搜索名称或代码" @keyup.enter="load" @clear="load" />
        <el-select v-model="filters.status" aria-label="兑换码状态" @change="load">
          <el-option v-for="item in statusOptions" :key="item.value" :label="item.label" :value="item.value" />
        </el-select>
        <el-button :icon="Search" :loading="loading" @click="load">查询</el-button>
      </div>
      <div class="spacer" />
      <el-button :icon="Trash2" :loading="deleting" :disabled="loading" @click="removeInvalid">删除无效码</el-button>
      <el-button type="primary" :icon="Plus" @click="openCreate">创建兑换码</el-button>
    </div>

    <div class="table-panel">
      <ResponsiveList v-if="loading || codes.length">
        <template #desktop><el-table v-loading="loading" :data="codes" row-key="id">
        <el-table-column label="名称" min-width="160"><template #default="{ row }"><strong class="code-name">{{ row.name }}</strong></template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><span class="status-dot" :class="statusClass(row.status)">{{ statusLabel(row.status) }}</span></template></el-table-column>
        <el-table-column label="代码" min-width="270"><template #default="{ row }"><div class="code-cell"><code>{{ row.code }}</code><TableActionButton :icon="Copy" label="复制兑换码" :size="15" @click="copyCode(row.code)" /></div></template></el-table-column>
        <el-table-column label="额度" min-width="120"><template #default="{ row }"><strong class="amount">{{ formatCost(row.amount) }}</strong></template></el-table-column>
        <el-table-column label="创建时间" min-width="170"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="过期时间" min-width="170"><template #default="{ row }">{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</template></el-table-column>
        <el-table-column label="兑换人" min-width="170"><template #default="{ row }"><div class="redeemer"><span>{{ row.redeemedByName || '—' }}</span><small v-if="row.redeemedAt">{{ formatTime(row.redeemedAt) }}</small></div></template></el-table-column>
        </el-table></template>
        <template #mobile><MobileRecordList :loading="loading">
          <article v-for="row in codes" :key="row.id" class="mobile-record">
            <div class="mobile-record__header"><div class="mobile-record__title"><strong>{{ row.name }}</strong><small>创建于 {{ formatTime(row.createdAt) }}</small></div><span class="status-dot" :class="statusClass(row.status)">{{ statusLabel(row.status) }}</span></div>
            <div class="mobile-record__code"><code>{{ row.code }}</code><TableActionButton :icon="Copy" label="复制兑换码" :size="15" @click="copyCode(row.code)" /></div>
            <dl class="mobile-record__facts"><div><dt>兑换额度</dt><dd class="mono">{{ formatCost(row.amount) }}</dd></div><div><dt>过期时间</dt><dd>{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</dd></div><div class="mobile-record__wide"><dt>兑换人</dt><dd>{{ row.redeemedByName || '尚未兑换' }}<template v-if="row.redeemedAt"> · {{ formatTime(row.redeemedAt) }}</template></dd></div></dl>
          </article>
        </MobileRecordList></template>
      </ResponsiveList>
      <div v-else class="empty-state">
        <div><Ticket :size="28" /><strong>{{ filters.keyword || filters.status !== 'all' ? '没有匹配的兑换码' : '还没有兑换码' }}</strong><span>{{ filters.keyword || filters.status !== 'all' ? '调整搜索条件后重新查询' : '创建后可将额度发放给用户' }}</span><el-button v-if="!filters.keyword && filters.status === 'all'" type="primary" :icon="Plus" @click="openCreate">创建兑换码</el-button></div>
      </div>
    </div>

    <el-dialog v-model="dialogOpen" title="创建兑换码" width="min(560px, 94vw)" :close-on-click-modal="!createdCodes.length">
      <div v-if="createdCodes.length" class="created-result">
        <div class="created-heading"><span class="result-mark"><Ticket :size="18" /></span><div><strong>兑换码已创建</strong><small>完整代码仅在这里集中展示，请按需分发。</small></div></div>
        <div class="created-list">
          <div v-for="item in createdCodes" :key="item.id" class="created-row"><code>{{ item.code }}</code><span>{{ formatCost(item.amount) }}</span><TableActionButton :icon="Copy" label="复制兑换码" @click="copyCode(item.code)" /></div>
        </div>
      </div>
      <el-form v-else label-position="top">
        <el-form-item label="名称"><el-input v-model="form.name" maxlength="20" show-word-limit placeholder="例如 新用户活动" /></el-form-item>
        <div class="form-grid">
          <el-form-item label="兑换额度（USD）"><el-input-number v-model="form.amount" :min="0.00000001" :precision="8" :controls="false" placeholder="0.00" style="width: 100%" /></el-form-item>
          <el-form-item label="批量数量"><el-input-number v-model="form.quantity" :min="1" :max="100" :step="1" step-strictly style="width: 100%" /></el-form-item>
        </div>
        <el-form-item label="过期时间">
          <el-radio-group v-model="form.expiry" class="expiry-options">
            <el-radio-button value="never">永不过期</el-radio-button>
            <el-radio-button value="month">1 个月</el-radio-button>
            <el-radio-button value="week">1 周</el-radio-button>
            <el-radio-button value="day">1 天</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer><el-button v-if="createdCodes.length" type="primary" @click="dialogOpen = false">完成</el-button><template v-else><el-button @click="dialogOpen = false">取消</el-button><el-button type="primary" :icon="Plus" :loading="saving" @click="create">创建兑换码</el-button></template></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.filter-group .el-input { width: 260px; }.filter-group .el-select { width: 132px; }.code-name { color: #15202b; font-size: 12px; }.code-cell { display: flex; min-width: 0; align-items: center; gap: 8px; }.code-cell code { min-width: 0; flex: 1; overflow: hidden; color: #245f96; font-family: 'JetBrains Mono', monospace; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.amount { color: #16866f; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.redeemer { display: flex; flex-direction: column; gap: 2px; }.redeemer small { color: #7b8792; font-size: 10px; }.empty-state svg { display: block; margin: 0 auto 10px; color: #7b8792; }.empty-state span { display: block; margin-bottom: 14px; }.created-result { display: flex; flex-direction: column; gap: 14px; }.created-heading { display: flex; align-items: center; gap: 10px; }.created-heading > div { display: flex; flex-direction: column; gap: 3px; }.created-heading small { color: #66717d; font-size: 11px; }.result-mark { display: grid; width: 36px; height: 36px; place-items: center; border: 1px solid #acd7cc; border-radius: 5px; color: #16866f; background: #e5f5f1; }.created-list { max-height: 360px; overflow: auto; border: 1px solid #dce2e7; border-radius: 6px; }.created-row { display: grid; grid-template-columns: minmax(0, 1fr) auto 34px; min-height: 48px; align-items: center; gap: 10px; padding: 6px 8px 6px 12px; border-bottom: 1px solid #e4e9ed; }.created-row:last-child { border-bottom: 0; }.created-row code { overflow-wrap: anywhere; color: #15202b; font-family: 'JetBrains Mono', monospace; font-size: 11px; }.created-row > span { color: #16866f; font-family: 'JetBrains Mono', monospace; font-size: 11px; }.expiry-options { display: flex; width: 100%; }.expiry-options :deep(.el-radio-button) { flex: 1; }.expiry-options :deep(.el-radio-button__inner) { width: 100%; padding-inline: 8px; }
@media (max-width: 899px) { .redemption-toolbar { align-items: stretch; }.filter-group { flex: 1 1 100%; }.filter-group .el-input { flex: 1 1 220px; width: auto; }.filter-group .el-select { flex: 0 0 132px; }.redemption-toolbar > .el-button { margin-left: 0; } }
@media (max-width: 560px) { .filter-group .el-input, .filter-group .el-select, .filter-group > .el-button { width: 100%; flex: 1 1 100%; }.redemption-toolbar > .el-button { flex: 1; }.expiry-options { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }.expiry-options :deep(.el-radio-button__inner) { border-left: var(--el-border); border-radius: 0; }.created-row { grid-template-columns: minmax(0, 1fr) 34px; }.created-row > span { grid-column: 1; grid-row: 2; }.created-row :deep(.action-button-wrap) { grid-column: 2; grid-row: 1 / 3; } }
</style>
