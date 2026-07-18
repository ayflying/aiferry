<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { CircleAlert, Coins, KeyRound, Plus, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelCostResult, ChannelCredential, CostSummary } from '../api/types'
import { showError } from '../lib/error'
import { formatCost, formatTime } from '../lib/format'

const props = defineProps<{ modelValue: boolean; channel?: Channel }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; changed: [] }>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const loading = ref(false)
const adding = ref(false)
const querying = ref(false)
const credentialValue = ref('')
const rows = ref<ChannelCredential[]>([])
const queryDetails = ref<ChannelCostResult['credentials']>([])
const summaries = ref<CostSummary[]>([])
const drawerSize = window.innerWidth <= 600 ? '94%' : '760px'

watch(() => props.modelValue, (open) => {
  if (open) void load(true)
})
watch(() => props.channel?.id, () => {
  if (visible.value) void load(true)
})

async function load(resetDetails = false) {
  if (!props.channel) return
  loading.value = true
  if (resetDetails) {
    queryDetails.value = []
    summaries.value = props.channel.costSummaries || []
  }
  try {
    rows.value = await apiGet<ChannelCredential[]>(`/channels/${props.channel.id}/credentials`)
  } catch (error) {
    showError(error, '加载上游密钥失败')
  } finally {
    loading.value = false
  }
}

async function addCredential() {
  if (!props.channel || !credentialValue.value.trim()) return
  adding.value = true
  try {
    await apiPost(`/channels/${props.channel.id}/credentials`, { apiKey: credentialValue.value.trim() })
    credentialValue.value = ''
    ElMessage.success('上游密钥已追加')
    await load(true)
    emit('changed')
  } catch (error) {
    showError(error, '追加上游密钥失败')
  } finally {
    adding.value = false
  }
}

async function setStatus(item: ChannelCredential, enabled: boolean) {
  if (!props.channel) return
  try {
    await apiPut(`/channels/${props.channel.id}/credentials/${item.id}/status`, { status: enabled ? 1 : 0 })
    item.status = enabled ? 1 : 0
    if (enabled) {
      item.autoDisabled = false
      item.autoDisabledReason = ''
    }
    ElMessage.success(enabled ? '上游密钥已启用' : '上游密钥已停用')
    emit('changed')
  } catch (error) {
    showError(error, '更新上游密钥状态失败')
  }
}

async function remove(item: ChannelCredential) {
  if (!props.channel) return
  try {
    await ElMessageBox.confirm(`删除上游密钥“${item.keyPrefix}”？固定使用该密钥的访问密钥会在下次请求重新选择。`, '删除上游密钥', {
      type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消',
    })
    await apiDelete(`/channels/${props.channel.id}/credentials/${item.id}`)
    ElMessage.success('上游密钥已删除')
    await load(true)
    emit('changed')
  } catch (error) {
    if (error !== 'cancel') showError(error, '删除上游密钥失败')
  }
}

async function queryCosts() {
  if (!props.channel) return
  querying.value = true
  try {
    const result = await apiPost<ChannelCostResult>(`/channels/${props.channel.id}/costs/query`, {})
    queryDetails.value = result.credentials || []
    summaries.value = result.summaries || []
    const failures = queryDetails.value.filter((item) => item.error).length
    ElMessage.success(failures ? `费用查询完成，${failures} 个密钥失败` : '费用查询完成')
    await load(false)
    emit('changed')
  } catch (error) {
    showError(error, '查询上游费用失败')
  } finally {
    querying.value = false
  }
}

function statusText(item: ChannelCredential) {
  if (item.autoDisabled) return '自动禁用'
  return item.status === 1 ? '启用' : '手动停用'
}

function costDetail(item: ChannelCredential) {
  return queryDetails.value.find((detail) => detail.credentialId === item.id)
}
</script>

<template>
  <el-drawer v-model="visible" :title="`上游密钥 · ${channel?.name || ''}`" :size="drawerSize" destroy-on-close>
    <div class="credential-toolbar">
      <div class="credential-add">
        <el-input v-model="credentialValue" type="password" show-password autocomplete="new-password" placeholder="追加上游推理密钥" @keyup.enter="addCredential" />
        <el-button type="primary" :icon="Plus" :loading="adding" :disabled="!credentialValue.trim()" @click="addCredential">追加</el-button>
      </div>
      <el-button :icon="Coins" :loading="querying" :disabled="channel?.costQueryMode === 'none'" @click="queryCosts">查询余额</el-button>
    </div>

    <div v-if="summaries.length" class="cost-summary-grid">
      <div v-for="summary in summaries" :key="summary.currency" class="summary-item">
        <strong>{{ summary.currency }}</strong>
        <span v-if="summary.usedAmount !== undefined">已用 {{ formatCost(summary.usedAmount, summary.currency) }}</span>
        <span v-if="summary.remainingAmount !== undefined">余额 {{ formatCost(summary.remainingAmount, summary.currency) }}</span>
      </div>
    </div>

    <div v-loading="loading" class="credential-table">
      <el-table :data="rows" row-key="id" size="small">
        <el-table-column label="上游密钥" min-width="145"><template #default="{ row }"><span class="mono key-prefix"><KeyRound :size="14" />{{ row.keyPrefix }}</span></template></el-table-column>
        <el-table-column label="状态" width="118"><template #default="{ row }"><el-tooltip v-if="row.autoDisabled" :content="row.autoDisabledReason || '该密钥被系统自动禁用'"><span class="status-dot warning">自动禁用</span></el-tooltip><span v-else class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ statusText(row) }}</span></template></el-table-column>
        <el-table-column label="费用与余额" min-width="200"><template #default="{ row }"><div class="cost-state"><template v-if="costDetail(row)?.error"><span class="danger-text">{{ costDetail(row)?.error }}</span></template><template v-else><span v-if="row.lastCostUsed !== undefined">已用 {{ formatCost(row.lastCostUsed, row.lastCostCurrency) }}</span><span v-if="row.lastCostRemaining !== undefined">余额 {{ formatCost(row.lastCostRemaining, row.lastCostCurrency) }}</span><small v-if="row.lastCostAt">{{ formatTime(row.lastCostAt) }}</small><span v-if="row.lastCostUsed === undefined && row.lastCostRemaining === undefined" class="muted">尚未查询</span></template></div></template></el-table-column>
        <el-table-column label="启用" width="76" align="center"><template #default="{ row }"><el-switch :model-value="row.status === 1" @update:model-value="setStatus(row, $event)" /></template></el-table-column>
        <el-table-column label="操作" width="62" align="center"><template #default="{ row }"><el-tooltip content="删除上游密钥"><button class="icon-button danger" type="button" :aria-label="`删除 ${row.keyPrefix}`" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip></template></el-table-column>
      </el-table>
      <div v-if="!loading && !rows.length" class="credential-empty"><CircleAlert :size="18" /><span>当前渠道没有可管理的上游密钥</span></div>
    </div>

    <div v-if="queryDetails.some(item => item.shared)" class="shared-balance">
      <strong>管理密钥共享余额</strong>
      <span v-for="item in queryDetails.filter(detail => detail.shared)" :key="item.queriedAt">{{ item.remainingAmount === undefined ? '未返回余额' : formatCost(item.remainingAmount, item.currency) }}</span>
    </div>
  </el-drawer>
</template>

<style scoped>
.credential-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }.credential-add { display: flex; min-width: 0; flex: 1; gap: 8px; }.cost-summary-grid { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 14px; }.summary-item { display: flex; align-items: center; gap: 9px; padding: 7px 10px; border: 1px solid #dce2e7; border-radius: 6px; background: #fff; font-size: 11px; }.summary-item strong { color: #15202b; font-family: 'JetBrains Mono', monospace; }.cost-state { display: flex; min-width: 0; flex-direction: column; gap: 2px; font-size: 11px; }.cost-state small { color: #7b8792; }.key-prefix { display: inline-flex; align-items: center; gap: 6px; }.credential-empty { display: flex; min-height: 170px; align-items: center; justify-content: center; gap: 8px; color: #7b8792; font-size: 12px; }.shared-balance { display: flex; align-items: center; gap: 8px; margin-top: 14px; padding: 10px; border: 1px solid #c6dae9; border-radius: 6px; color: #40505f; background: #f4f9fd; font-size: 12px; }.shared-balance strong { color: #15202b; }@media (max-width: 600px) { .credential-toolbar { align-items: stretch; flex-direction: column; }.credential-add { width: 100%; }.credential-table { overflow-x: auto; }.credential-table :deep(.el-table) { min-width: 610px; } }
</style>
