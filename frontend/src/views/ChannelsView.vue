<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Coins, Pencil, Plus, RefreshCw, ScanSearch, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelInput, CostQueryConfig } from '../api/types'
import { useAppStore } from '../stores/app'
import { formatCost, formatTime } from '../lib/format'

const store = useAppStore()
const loading = ref(false)
const saving = ref(false)
const drawerOpen = ref(false)
const editingId = ref<number>()

const emptyConfig = (): CostQueryConfig => ({
  url: '', authType: 'none', headerName: 'Authorization', usedPath: '', remainingPath: '', currencyPath: '', fixedCurrency: 'USD',
})
const emptyForm = (): ChannelInput => ({
  name: '', baseUrl: 'https://api.openai.com/v1', apiKey: '', managementKey: '', organizationId: '', projectId: '',
  status: 1, priority: 0, weight: 1, costQueryMode: 'none', costQueryConfig: emptyConfig(),
})
const form = reactive<ChannelInput>(emptyForm())
const title = computed(() => editingId.value ? '编辑渠道' : '添加渠道')

async function load() {
  loading.value = true
  try { await store.loadChannels() } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}

function openCreate() {
  editingId.value = undefined
  Object.assign(form, emptyForm())
  drawerOpen.value = true
}

function openEdit(channel: Channel) {
  editingId.value = channel.id
  Object.assign(form, {
    name: channel.name, baseUrl: channel.baseUrl, apiKey: '', managementKey: undefined,
    organizationId: channel.organizationId, projectId: channel.projectId, status: channel.status,
    priority: channel.priority, weight: channel.weight, costQueryMode: channel.costQueryMode,
    costQueryConfig: { ...emptyConfig(), ...(channel.costQueryConfig || {}) },
  })
  drawerOpen.value = true
}

async function save() {
  if (!form.name.trim() || !form.baseUrl.trim() || (!editingId.value && !form.apiKey?.trim())) {
    ElMessage.warning('请填写渠道名称、API 根地址和密钥')
    return
  }
  saving.value = true
  try {
    const payload: ChannelInput = JSON.parse(JSON.stringify(form))
    if (editingId.value && !payload.apiKey) delete payload.apiKey
    if (editingId.value && payload.managementKey === undefined) delete payload.managementKey
    if (editingId.value) await apiPut(`/channels/${editingId.value}`, payload)
    else await apiPost('/channels', payload)
    ElMessage.success(editingId.value ? '渠道已更新' : '渠道已添加')
    drawerOpen.value = false
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { saving.value = false }
}

async function discover(channel: Channel) {
  loading.value = true
  try {
    const models = await apiPost<string[]>(`/channels/${channel.id}/models/discover`)
    ElMessage.success(`发现 ${models.length} 个模型`)
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}

async function queryCost(channel: Channel) {
  loading.value = true
  try {
    const data = await apiPost<{ usedAmount?: number; remainingAmount?: number; currency: string }>(`/channels/${channel.id}/costs/query`, {})
    const parts = [data.usedAmount === undefined ? '' : `已用 ${formatCost(data.usedAmount, data.currency)}`, data.remainingAmount === undefined ? '' : `剩余 ${formatCost(data.remainingAmount, data.currency)}`].filter(Boolean)
    ElMessage.success(parts.join('，') || '费用查询完成')
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}

async function remove(channel: Channel) {
  try {
    await ElMessageBox.confirm(`删除渠道“${channel.name}”？`, '删除渠道', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/channels/${channel.id}`)
    ElMessage.success('渠道已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel') ElMessage.error((error as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="muted">管理 OpenAI 兼容上游、路由顺序和费用查询</div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="openCreate">添加渠道</el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="store.channels" row-key="id">
        <el-table-column label="渠道" min-width="190">
          <template #default="{ row }"><div class="channel-name"><strong>{{ row.name }}</strong><span>{{ row.baseUrl }}</span></div></template>
        </el-table-column>
        <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column>
        <el-table-column label="路由" width="108"><template #default="{ row }"><span class="mono">P{{ row.priority }} / W{{ row.weight }}</span></template></el-table-column>
        <el-table-column label="模型" width="110"><template #default="{ row }">{{ row.enabledModelCount }} / {{ row.discoveredModels }}</template></el-table-column>
        <el-table-column label="最近测试" min-width="140"><template #default="{ row }"><span v-if="row.lastTestStatus" class="status-dot" :class="row.lastTestStatus">{{ row.lastTestStatus === 'success' ? `${row.lastTestLatencyMs} ms` : '失败' }}</span><span v-else class="muted">未测试</span></template></el-table-column>
        <el-table-column label="上游费用" min-width="150"><template #default="{ row }"><div v-if="row.lastCostAt" class="cost-cell"><span v-if="row.lastCostUsed !== undefined">已用 {{ formatCost(row.lastCostUsed, row.lastCostCurrency) }}</span><span v-if="row.lastCostRemaining !== undefined">剩余 {{ formatCost(row.lastCostRemaining, row.lastCostCurrency) }}</span><small>{{ formatTime(row.lastCostAt) }}</small></div><span v-else class="muted">未查询</span></template></el-table-column>
        <el-table-column label="操作" width="190" fixed="right" align="right">
          <template #default="{ row }"><div class="table-actions">
            <el-tooltip content="发现模型"><button class="icon-button" @click="discover(row)"><ScanSearch :size="16" /></button></el-tooltip>
            <el-tooltip content="查询费用"><button class="icon-button" :disabled="row.costQueryMode === 'none'" @click="queryCost(row)"><Coins :size="16" /></button></el-tooltip>
            <el-tooltip content="编辑"><button class="icon-button" @click="openEdit(row)"><Pencil :size="16" /></button></el-tooltip>
            <el-tooltip content="删除"><button class="icon-button danger" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip>
          </div></template>
        </el-table-column>
      </el-table>
      <div v-if="!loading && !store.channels.length" class="empty-state"><div><strong>还没有渠道</strong><span>添加第一个 OpenAI 兼容上游</span></div></div>
    </div>

    <el-drawer v-model="drawerOpen" :title="title" size="min(620px, 94vw)">
      <el-form label-position="top">
        <div class="form-grid">
          <el-form-item label="渠道名称"><el-input v-model="form.name" placeholder="例如 OpenAI 主线路" /></el-form-item>
          <el-form-item label="API 根地址"><el-input v-model="form.baseUrl" placeholder="https://api.openai.com/v1" /></el-form-item>
          <el-form-item label="推理密钥"><el-input v-model="form.apiKey" type="password" show-password :placeholder="editingId ? '留空则保持不变' : 'sk-...'" autocomplete="new-password" /></el-form-item>
          <el-form-item label="管理密钥"><el-input v-model="form.managementKey" type="password" show-password :placeholder="editingId ? '留空则清除；不修改请勿聚焦' : '仅 OpenAI Costs 需要'" autocomplete="new-password" /></el-form-item>
          <el-form-item label="组织 ID"><el-input v-model="form.organizationId" clearable /></el-form-item>
          <el-form-item label="项目 ID"><el-input v-model="form.projectId" clearable /></el-form-item>
          <el-form-item label="优先级"><el-input-number v-model="form.priority" :min="-999" :max="999" controls-position="right" /></el-form-item>
          <el-form-item label="权重"><el-input-number v-model="form.weight" :min="1" :max="1000" controls-position="right" /></el-form-item>
          <el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item>
          <el-form-item label="费用查询"><el-select v-model="form.costQueryMode"><el-option label="不查询" value="none" /><el-option label="OpenAI 组织 Costs" value="openai_costs" /><el-option label="Sub2API Usage" value="sub2api_usage" /><el-option label="自定义 JSON" value="custom_json" /></el-select></el-form-item>
        </div>
        <div v-if="form.costQueryMode === 'custom_json'" class="custom-query">
          <div class="section-heading"><h2>自定义费用字段</h2><span>只读 GET JSON</span></div>
          <div class="form-grid">
            <el-form-item class="full" label="查询地址"><el-input v-model="form.costQueryConfig.url" placeholder="/usage 或完整 HTTPS 地址" /></el-form-item>
            <el-form-item label="鉴权密钥"><el-select v-model="form.costQueryConfig.authType"><el-option label="无鉴权" value="none" /><el-option label="推理密钥" value="channel_key" /><el-option label="管理密钥" value="management_key" /></el-select></el-form-item>
            <el-form-item label="鉴权 Header"><el-input v-model="form.costQueryConfig.headerName" /></el-form-item>
            <el-form-item label="已用金额路径"><el-input v-model="form.costQueryConfig.usedPath" placeholder="usage.total.cost" /></el-form-item>
            <el-form-item label="剩余额度路径"><el-input v-model="form.costQueryConfig.remainingPath" placeholder="remaining" /></el-form-item>
            <el-form-item label="币种路径"><el-input v-model="form.costQueryConfig.currencyPath" placeholder="currency" /></el-form-item>
            <el-form-item label="固定币种"><el-input v-model="form.costQueryConfig.fixedCurrency" placeholder="USD" /></el-form-item>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="drawerOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存渠道</el-button></template>
    </el-drawer>
  </div>
</template>

<style scoped>
.channel-name { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.channel-name strong { font-size: 13px; }.channel-name span { overflow: hidden; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.cost-cell { display: flex; flex-direction: column; gap: 2px; font-size: 11px; }.cost-cell small { color: #7b8792; }.custom-query { margin-top: 8px; padding-top: 16px; border-top: 1px solid #dce2e7; }
</style>
