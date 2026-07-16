<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Coins, FlaskConical, Pencil, Plus, RefreshCw, ScanSearch, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelInput, ChannelModel, CostQueryConfig, DiscoveredModel, ModelTestResult } from '../api/types'
import { useAppStore } from '../stores/app'
import { formatCost, formatTime } from '../lib/format'
import { enabledChannelModels, sortDiscoveredModels } from '../lib/models'

const store = useAppStore()
const loading = ref(false)
const saving = ref(false)
const drawerOpen = ref(false)
const editingId = ref<number>()
const discoveryOpen = ref(false)
const discovering = ref(false)
const applyingSelection = ref(false)
const discoveryChannel = ref<Channel>()
const discoveredModels = ref<DiscoveredModel[]>([])
const discoveryKeyword = ref('')
const selectedModelNames = ref<string[]>([])
const testOpen = ref(false)
const testLoading = ref(false)
const testChannel = ref<Channel>()
const testModels = ref<ChannelModel[]>([])
const testModelId = ref<number>()
const testEndpoint = ref<ModelTestResult['endpoint']>('chat')
const testResult = ref<ModelTestResult>()

const visibleDiscoveredModels = computed(() => {
  const keyword = discoveryKeyword.value.trim().toLowerCase()
  if (!keyword) return discoveredModels.value
  return discoveredModels.value.filter((item) => item.name.toLowerCase().includes(keyword))
})
const allVisibleSelected = computed(() => visibleDiscoveredModels.value.length > 0
  && visibleDiscoveredModels.value.every((item) => selectedModelNames.value.includes(item.name)))

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
  discoveryChannel.value = channel
  discoveryKeyword.value = ''
  discoveredModels.value = []
  selectedModelNames.value = []
  discoveryOpen.value = true
  discovering.value = true
  try {
    const models = await apiPost<DiscoveredModel[]>(`/channels/${channel.id}/models/discover`)
    discoveredModels.value = sortDiscoveredModels(models)
    selectedModelNames.value = discoveredModels.value.filter((item) => item.selected).map((item) => item.name)
  } catch (error) {
    discoveryOpen.value = false
    ElMessage.error((error as Error).message)
  } finally { discovering.value = false }
}

function toggleVisibleModels() {
  const selected = new Set(selectedModelNames.value)
  for (const model of visibleDiscoveredModels.value) {
    if (allVisibleSelected.value) selected.delete(model.name)
    else selected.add(model.name)
  }
  selectedModelNames.value = [...selected]
}

async function saveModelSelection() {
  if (!discoveryChannel.value) return
  applyingSelection.value = true
  try {
    await apiPut(`/channels/${discoveryChannel.value.id}/models/selection`, { modelNames: selectedModelNames.value })
    ElMessage.success(`已启用 ${selectedModelNames.value.length} 个模型`)
    discoveryOpen.value = false
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { applyingSelection.value = false }
}

async function openTest(channel: Channel) {
  testChannel.value = channel
  testModels.value = []
  testModelId.value = undefined
  testEndpoint.value = 'chat'
  testResult.value = undefined
  testOpen.value = true
  testLoading.value = true
  try {
    const models = await apiGet<ChannelModel[]>(`/channels/${channel.id}/models`)
    testModels.value = enabledChannelModels(models)
    testModelId.value = testModels.value[0]?.id
  } catch (error) { ElMessage.error((error as Error).message) } finally { testLoading.value = false }
}

async function testModel() {
  if (!testModelId.value) return
  testLoading.value = true
  testResult.value = undefined
  try {
    testResult.value = await apiPost<ModelTestResult>('/models/test', { modelId: testModelId.value, endpoint: testEndpoint.value })
    if (testResult.value.success) ElMessage.success('模型测试通过')
    else ElMessage.error(testResult.value.message || '模型测试失败')
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { testLoading.value = false }
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
        <el-table-column label="操作" width="230" fixed="right" align="right">
          <template #default="{ row }"><div class="table-actions">
            <el-tooltip content="发现模型"><button class="icon-button" type="button" :aria-label="`发现 ${row.name} 的模型`" @click="discover(row)"><ScanSearch :size="16" /></button></el-tooltip>
            <el-tooltip content="测试模型"><button class="icon-button" type="button" :aria-label="`测试 ${row.name} 的模型`" @click="openTest(row)"><FlaskConical :size="16" /></button></el-tooltip>
            <el-tooltip content="查询费用"><button class="icon-button" type="button" :aria-label="`查询 ${row.name} 的费用`" :disabled="row.costQueryMode === 'none'" @click="queryCost(row)"><Coins :size="16" /></button></el-tooltip>
            <el-tooltip content="编辑"><button class="icon-button" type="button" :aria-label="`编辑渠道 ${row.name}`" @click="openEdit(row)"><Pencil :size="16" /></button></el-tooltip>
            <el-tooltip content="删除"><button class="icon-button danger" type="button" :aria-label="`删除渠道 ${row.name}`" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip>
          </div></template>
        </el-table-column>
      </el-table>
      <div v-if="!loading && !store.channels.length" class="empty-state"><div><strong>还没有渠道</strong><span>添加第一个 OpenAI 兼容上游</span></div></div>
    </div>

    <el-dialog v-model="discoveryOpen" :title="`选择模型 · ${discoveryChannel?.name || ''}`" width="min(640px, 94vw)">
      <div v-loading="discovering" class="model-selection">
        <div class="selection-toolbar">
          <el-input v-model="discoveryKeyword" clearable placeholder="搜索模型" />
          <el-button :disabled="!visibleDiscoveredModels.length" @click="toggleVisibleModels">{{ allVisibleSelected ? '取消当前结果' : '选择当前结果' }}</el-button>
        </div>
        <div class="selection-summary"><span>已选择 {{ selectedModelNames.length }} 个</span><span>共发现 {{ discoveredModels.length }} 个</span></div>
        <el-checkbox-group v-if="visibleDiscoveredModels.length" v-model="selectedModelNames" class="model-check-list">
          <el-checkbox v-for="item in visibleDiscoveredModels" :key="item.name" :value="item.name"><code>{{ item.name }}</code></el-checkbox>
        </el-checkbox-group>
        <div v-else-if="!discovering" class="selection-empty">{{ discoveredModels.length ? '没有匹配模型' : '上游没有返回模型' }}</div>
      </div>
      <template #footer><el-button @click="discoveryOpen = false">取消</el-button><el-button type="primary" :loading="applyingSelection" :disabled="discovering" @click="saveModelSelection">确认选择</el-button></template>
    </el-dialog>

    <el-dialog v-model="testOpen" :title="`渠道测试 · ${testChannel?.name || ''}`" width="min(540px, 94vw)">
      <div v-loading="testLoading" class="test-dialog">
        <template v-if="testModels.length">
          <el-form label-position="top">
            <el-form-item label="已启用模型"><el-select v-model="testModelId" filterable><el-option v-for="model in testModels" :key="model.id" :label="model.publicName" :value="model.id"><span class="model-option"><strong>{{ model.publicName }}</strong><small>{{ model.upstreamName }}</small></span></el-option></el-select></el-form-item>
            <el-form-item label="接口类型"><el-segmented v-model="testEndpoint" :options="[{ label: 'Chat', value: 'chat' }, { label: 'Responses', value: 'responses' }, { label: 'Embeddings', value: 'embeddings' }]" /></el-form-item>
          </el-form>
          <div v-if="testResult" class="test-result" :class="testResult.success ? 'success' : 'failed'">
            <strong>{{ testResult.success ? '测试通过' : '测试失败' }}</strong>
            <span>HTTP {{ testResult.httpStatus || '—' }} · {{ testResult.latencyMs }} ms · 输入 {{ testResult.inputTokens }} · 输出 {{ testResult.outputTokens }}</span>
            <p>{{ testResult.message }}</p>
          </div>
        </template>
        <div v-else-if="!testLoading" class="selection-empty">当前渠道没有已选择的模型</div>
      </div>
      <template #footer><el-button @click="testOpen = false">关闭</el-button><el-button type="primary" :loading="testLoading" :disabled="!testModelId" @click="testModel">开始测试</el-button></template>
    </el-dialog>

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
.channel-name { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.channel-name strong { font-size: 13px; }.channel-name span { overflow: hidden; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.cost-cell { display: flex; flex-direction: column; gap: 2px; font-size: 11px; }.cost-cell small { color: #7b8792; }.model-selection { min-height: 300px; }.selection-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }.selection-summary { display: flex; justify-content: space-between; margin: 13px 0 8px; color: #66717d; font-size: 11px; }.model-check-list { display: grid; max-height: 390px; overflow-y: auto; border-block: 1px solid #dce2e7; }.model-check-list .el-checkbox { min-width: 0; height: 38px; margin: 0; padding: 0 10px; border-bottom: 1px solid #eef1f3; }.model-check-list .el-checkbox:last-child { border-bottom: 0; }.model-check-list code { overflow-wrap: anywhere; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.selection-empty { display: grid; min-height: 220px; place-items: center; color: #66717d; font-size: 12px; }.test-dialog { min-height: 180px; }.test-result { padding: 13px; border: 1px solid #dce2e7; border-radius: 6px; }.test-result.success { border-color: #acd7cc; background: #f2faf8; }.test-result.failed { border-color: #e9abb2; background: #fff6f7; }.test-result span { display: block; margin-top: 5px; color: #66717d; font-size: 11px; }.test-result p { margin: 8px 0 0; font-size: 12px; }.model-option { display: flex; align-items: center; justify-content: space-between; gap: 16px; }.model-option small { color: #7b8792; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.custom-query { margin-top: 8px; padding-top: 16px; border-top: 1px solid #dce2e7; }
@media (max-width: 600px) { .selection-toolbar { grid-template-columns: 1fr; }.model-option small { display: none; } }
</style>
