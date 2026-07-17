<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Braces, Coins, FlaskConical, Network, Pencil, Plus, RefreshCw, ScanSearch, Tags, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelGroup, ChannelInput, ChannelType, ChannelTypeConfig, DiscoveredModel } from '../api/types'
import ChannelModelTestDialog from '../components/ChannelModelTestDialog.vue'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { formatCost, formatLatency, formatTime } from '../lib/format'
import { sortDiscoveredModels } from '../lib/models'

const store = useAppStore()
const activeTab = ref('channels')
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
const discoveryError = ref('')
const testOpen = ref(false)
const testChannel = ref<Channel>()
const typeDrawerOpen = ref(false)
const typeSaving = ref(false)
const editingType = ref<ChannelType>()
const typeForm = reactive({ name: '', code: '', status: 1, configText: '' })
const groupDrawerOpen = ref(false)
const groupSaving = ref(false)
const editingGroup = ref<ChannelGroup>()
const groupForm = reactive({ name: '', code: '', description: '', status: 1, channelIds: [] as number[] })

const drawerSize = window.innerWidth <= 600 ? '94%' : '620px'
const typeDrawerSize = window.innerWidth <= 600 ? '94%' : '680px'
const activeTypes = computed(() => store.channelTypes.filter((item) => item.status === 1 || item.code === form.type))
const selectedChannelType = computed(() => store.channelTypes.find((item) => item.code === form.type))
const usesManagementKey = computed(() => {
  const config = selectedChannelType.value?.config
  if (!config) return false
  return [config.models.authType, config.costs.authType, config.pricing.authType].includes('management_key')
})
const visibleDiscoveredModels = computed(() => {
  const keyword = discoveryKeyword.value.trim().toLowerCase()
  if (!keyword) return discoveredModels.value
  return discoveredModels.value.filter((item) => item.name.toLowerCase().includes(keyword))
})
const allVisibleSelected = computed(() => visibleDiscoveredModels.value.length > 0
  && visibleDiscoveredModels.value.every((item) => selectedModelNames.value.includes(item.name)))

const emptyTypeConfig = (): ChannelTypeConfig => ({
  models: { method: 'GET', path: '/models', listPath: 'data', idPath: 'id', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' },
  costs: { adapter: 'none', method: 'GET', path: '', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ', usedPath: '', remainingPath: '', currencyPath: '', fixedCurrency: 'USD' },
  pricing: { adapter: 'none', method: 'GET', path: '', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ', listPath: '', modelPath: '', namePath: '', currencyPath: '', conditionsPath: '', ratesPath: '', inputPricePath: '', cachedInputPricePath: '', cacheWritePricePath: '', outputPricePath: '', imageInputPricePath: '', audioInputPricePath: '', audioOutputPricePath: '', requestPricePath: '' },
})
const emptyForm = (): ChannelInput => ({
  name: '', type: '', baseUrl: 'https://api.openai.com/v1', apiKey: '', managementKey: '', organizationId: '', projectId: '', status: 1, priority: 0, weight: 1, groupIds: [],
})
const form = reactive<ChannelInput>(emptyForm())
const title = computed(() => editingId.value ? '编辑渠道' : '添加渠道')

async function load() {
  loading.value = true
  try { await Promise.all([store.loadChannels(), store.loadChannelTypes(), store.loadChannelGroups()]) } catch (error) { showError(error, '加载渠道失败') } finally { loading.value = false }
}

function openCreate() {
  editingId.value = undefined
  Object.assign(form, emptyForm(), { type: store.channelTypes.find((item) => item.status === 1)?.code || '' })
  drawerOpen.value = true
}

function openEdit(channel: Channel) {
  editingId.value = channel.id
  Object.assign(form, {
    name: channel.name, type: channel.type, baseUrl: channel.baseUrl, apiKey: '', managementKey: undefined,
    organizationId: channel.organizationId, projectId: channel.projectId, status: channel.status,
    priority: channel.priority, weight: channel.weight, groupIds: channel.groupIds || [],
  })
  drawerOpen.value = true
}

async function save() {
  if (!form.name.trim() || !form.type || !form.baseUrl.trim() || (!editingId.value && !form.apiKey?.trim())) {
    showError('请填写渠道名称、渠道类型、API 根地址和密钥', '信息不完整')
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
  } catch (error) { showError(error, '保存渠道失败') } finally { saving.value = false }
}

async function discover(channel: Channel) {
  discoveryChannel.value = channel
  discoveryKeyword.value = ''
  discoveredModels.value = []
  selectedModelNames.value = []
  discoveryError.value = ''
  discoveryOpen.value = true
  discovering.value = true
  try {
    const models = await apiPost<DiscoveredModel[]>(`/channels/${channel.id}/models/discover`)
    discoveredModels.value = sortDiscoveredModels(models)
    selectedModelNames.value = discoveredModels.value.filter((item) => item.selected).map((item) => item.name)
  } catch (error) {
    discoveryError.value = describeDiscoveryError(error)
  } finally { discovering.value = false }
}

function describeDiscoveryError(error: unknown) {
  const message = error instanceof Error ? error.message : '网络请求失败'
  if (message.startsWith('上游每日用量额度已用尽')) return message
  if (message.includes('HTTP 429')) {
    return '上游返回 HTTP 429，当前请求受到限流或该密钥的可用配额不足。请稍后重试，或在上游确认配额和请求限制。'
  }
  return `上游模型接口调用失败：${message}`
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
  } catch (error) { showError(error, '保存模型选择失败') } finally { applyingSelection.value = false }
}

async function openTest(channel: Channel) {
  testChannel.value = channel
  testOpen.value = true
}

async function queryCost(channel: Channel) {
  loading.value = true
  try {
    const data = await apiPost<{ usedAmount?: number; remainingAmount?: number; currency: string }>(`/channels/${channel.id}/costs/query`, {})
    const parts = [data.usedAmount === undefined ? '' : `已用 ${formatCost(data.usedAmount, data.currency)}`, data.remainingAmount === undefined ? '' : `剩余 ${formatCost(data.remainingAmount, data.currency)}`].filter(Boolean)
    ElMessage.success(parts.join('，') || '费用查询完成')
    await load()
  } catch (error) { showError(error, '查询费用失败') } finally { loading.value = false }
}

async function remove(channel: Channel) {
  try {
    await ElMessageBox.confirm(`删除渠道“${channel.name}”？`, '删除渠道', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/channels/${channel.id}`)
    ElMessage.success('渠道已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel') showError(error, '删除渠道失败')
  }
}

function openCreateType() {
  editingType.value = undefined
  Object.assign(typeForm, { name: '', code: '', status: 1, configText: JSON.stringify(emptyTypeConfig(), null, 2) })
  typeDrawerOpen.value = true
}

function openEditType(item: ChannelType) {
  editingType.value = item
  Object.assign(typeForm, { name: item.name, code: item.code, status: item.status, configText: JSON.stringify(item.config, null, 2) })
  typeDrawerOpen.value = true
}

async function saveType() {
  let config: ChannelTypeConfig
  try { config = JSON.parse(typeForm.configText) } catch { showError('渠道类型 JSON 格式无效', '格式错误'); return }
  if (!typeForm.name.trim() || !typeForm.code.trim()) { showError('请填写类型名称和类型代码', '信息不完整'); return }
  typeSaving.value = true
  try {
    const payload = { name: typeForm.name, code: typeForm.code, status: typeForm.status, config }
    if (editingType.value) await apiPut(`/channel-types/${editingType.value.id}`, payload)
    else await apiPost('/channel-types', payload)
    ElMessage.success(editingType.value ? '渠道类型已更新' : '渠道类型已添加')
    typeDrawerOpen.value = false
    await load()
  } catch (error) { showError(error, '保存渠道类型失败') } finally { typeSaving.value = false }
}

async function removeType(item: ChannelType) {
  try {
    await ElMessageBox.confirm(`删除渠道类型“${item.name}”？`, '删除渠道类型', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/channel-types/${item.id}`)
    ElMessage.success('渠道类型已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel') showError(error, '删除渠道类型失败')
  }
}

function costLabel(item: ChannelType) {
  return { none: '不查询', openai_costs: 'OpenAI Costs', sub2api_usage: 'Sub2API Usage', custom_json: '自定义 JSON' }[item.config.costs.adapter] || item.config.costs.adapter
}

function openCreateGroup() {
  editingGroup.value = undefined
  Object.assign(groupForm, { name: '', code: '', description: '', status: 1, channelIds: [] })
  groupDrawerOpen.value = true
}

function openEditGroup(item: ChannelGroup) {
  editingGroup.value = item
  Object.assign(groupForm, { name: item.name, code: item.code, description: item.description, status: item.status, channelIds: [...item.channelIds] })
  groupDrawerOpen.value = true
}

async function saveGroup() {
  if (!groupForm.name.trim() || !groupForm.code.trim()) { showError('请填写分组名称和代码', '信息不完整'); return }
  groupSaving.value = true
  try {
    const payload = { ...groupForm }
    if (editingGroup.value) await apiPut(`/channel-groups/${editingGroup.value.id}`, payload)
    else await apiPost('/channel-groups', payload)
    ElMessage.success(editingGroup.value ? '渠道分组已更新' : '渠道分组已添加')
    groupDrawerOpen.value = false
    await load()
  } catch (error) { showError(error, '保存渠道分组失败') } finally { groupSaving.value = false }
}

async function removeGroup(item: ChannelGroup) {
  try {
    await ElMessageBox.confirm(`删除渠道分组“${item.name}”？`, '删除渠道分组', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/channel-groups/${item.id}`)
    ElMessage.success('渠道分组已删除')
    await load()
  } catch (error) { if (error !== 'cancel') showError(error, '删除渠道分组失败') }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <el-tabs v-model="activeTab" class="channel-tabs">
      <el-tab-pane name="channels">
        <template #label><span class="tab-label"><Network :size="15" />渠道</span></template>
        <div class="page-toolbar">
          <div class="muted">管理上游、模型选择、路由顺序和费用查询</div>
          <div class="spacer" />
          <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreate">添加渠道</el-button>
        </div>
        <div class="table-panel">
          <el-table v-loading="loading" :data="store.channels" row-key="id">
            <el-table-column label="渠道" min-width="180"><template #default="{ row }"><div class="channel-name"><strong>{{ row.name }}</strong><span>{{ row.baseUrl }}</span></div></template></el-table-column>
            <el-table-column label="类型" min-width="120"><template #default="{ row }"><div class="type-cell"><strong>{{ row.typeName }}</strong><code>{{ row.type }}</code></div></template></el-table-column>
            <el-table-column label="状态" width="112"><template #default="{ row }"><el-tooltip v-if="row.autoDisabled" :content="row.autoDisabledReason || '渠道被自动禁用'" placement="top"><span class="status-dot warning">自动禁用</span></el-tooltip><span v-else class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '手动停用' }}</span></template></el-table-column>
            <el-table-column label="路由" width="108"><template #default="{ row }"><span class="mono">P{{ row.priority }} / W{{ row.weight }}</span></template></el-table-column>
            <el-table-column label="模型" width="100"><template #default="{ row }">{{ row.enabledModelCount }} / {{ row.discoveredModels }}</template></el-table-column>
            <el-table-column label="最近测试" min-width="130"><template #default="{ row }"><span v-if="row.lastTestStatus" class="status-dot" :class="row.lastTestStatus">{{ row.lastTestStatus === 'success' ? formatLatency(row.lastTestLatencyMs) : '失败' }}</span><span v-else class="muted">未测试</span></template></el-table-column>
            <el-table-column label="上游费用" min-width="145"><template #default="{ row }"><div v-if="row.lastCostAt" class="cost-cell"><span v-if="row.lastCostUsed !== undefined">已用 {{ formatCost(row.lastCostUsed, row.lastCostCurrency) }}</span><span v-if="row.lastCostRemaining !== undefined">剩余 {{ formatCost(row.lastCostRemaining, row.lastCostCurrency) }}</span><small>{{ formatTime(row.lastCostAt) }}</small></div><span v-else class="muted">未查询</span></template></el-table-column>
            <el-table-column label="操作" width="230" fixed="right" align="right"><template #default="{ row }"><div class="table-actions">
              <el-tooltip content="发现模型"><button class="icon-button" type="button" :aria-label="`发现 ${row.name} 的模型`" @click="discover(row)"><ScanSearch :size="16" /></button></el-tooltip>
              <el-tooltip content="测试模型"><button class="icon-button" type="button" :aria-label="`测试 ${row.name} 的模型`" @click="openTest(row)"><FlaskConical :size="16" /></button></el-tooltip>
              <el-tooltip content="查询费用"><button class="icon-button" type="button" :aria-label="`查询 ${row.name} 的费用`" :disabled="row.costQueryMode === 'none'" @click="queryCost(row)"><Coins :size="16" /></button></el-tooltip>
              <el-tooltip content="编辑"><button class="icon-button" type="button" :aria-label="`编辑渠道 ${row.name}`" @click="openEdit(row)"><Pencil :size="16" /></button></el-tooltip>
              <el-tooltip content="删除"><button class="icon-button danger" type="button" :aria-label="`删除渠道 ${row.name}`" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip>
            </div></template></el-table-column>
          </el-table>
          <div v-if="!loading && !store.channels.length" class="empty-state"><div><strong>还没有渠道</strong><span>先添加渠道类型，再接入第一个上游</span></div></div>
        </div>
      </el-tab-pane>

      <el-tab-pane name="groups">
        <template #label><span class="tab-label"><Tags :size="15" />渠道分组</span></template>
        <div class="page-toolbar"><div class="muted">为密钥授权和路由策略维护渠道归属</div><div class="spacer" /><el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button><el-button type="primary" :icon="Plus" @click="openCreateGroup">添加分组</el-button></div>
        <div class="table-panel">
          <el-table v-loading="loading" :data="store.channelGroups" row-key="id">
            <el-table-column label="分组" min-width="180"><template #default="{ row }"><div class="type-cell"><strong>{{ row.name }}</strong><code>{{ row.code }}</code></div></template></el-table-column>
            <el-table-column prop="description" label="说明" min-width="220" />
            <el-table-column label="渠道" min-width="180"><template #default="{ row }"><el-tag v-for="channelId in row.channelIds" :key="channelId" size="small" class="group-channel-tag">{{ store.channels.find(item => item.id === channelId)?.name || `#${channelId}` }}</el-tag><span v-if="!row.channelIds.length" class="muted">未分配</span></template></el-table-column>
            <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column>
            <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="编辑"><button class="icon-button" type="button" @click="openEditGroup(row)"><Pencil :size="16" /></button></el-tooltip><el-tooltip content="删除"><button class="icon-button danger" type="button" @click="removeGroup(row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
          </el-table>
          <div v-if="!loading && !store.channelGroups.length" class="empty-state"><div><strong>还没有渠道分组</strong><span>创建分组后可对访问密钥限定可用渠道</span></div></div>
        </div>
      </el-tab-pane>

      <el-tab-pane name="types">
        <template #label><span class="tab-label"><Braces :size="15" />渠道类型</span></template>
        <div class="page-toolbar">
          <div class="muted">JSON 定义模型发现、余额接口、鉴权和字段路径</div>
          <div class="spacer" />
          <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreateType">添加渠道类型</el-button>
        </div>
        <div class="table-panel">
          <el-table v-loading="loading" :data="store.channelTypes" row-key="id">
            <el-table-column label="类型" min-width="170"><template #default="{ row }"><div class="type-cell"><strong>{{ row.name }}</strong><code>{{ row.code }}</code></div></template></el-table-column>
            <el-table-column label="模型接口" min-width="220"><template #default="{ row }"><span class="mono">{{ row.config.models.method }} {{ row.config.models.path }}</span></template></el-table-column>
            <el-table-column label="余额查询" min-width="160"><template #default="{ row }"><span>{{ costLabel(row) }}</span><small v-if="row.config.costs.path"> · {{ row.config.costs.path }}</small></template></el-table-column>
            <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column>
            <el-table-column label="来源" width="88"><template #default="{ row }"><span class="muted">{{ row.builtIn === 1 ? '内置' : '自定义' }}</span></template></el-table-column>
            <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="编辑类型"><button class="icon-button" type="button" :aria-label="`编辑渠道类型 ${row.name}`" @click="openEditType(row)"><Pencil :size="16" /></button></el-tooltip><el-tooltip :disabled="row.builtIn === 0" :content="row.builtIn === 1 ? '内置类型不可删除' : ''"><button class="icon-button danger" type="button" :disabled="row.builtIn === 1" :aria-label="`删除渠道类型 ${row.name}`" @click="removeType(row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
          </el-table>
          <div v-if="!loading && !store.channelTypes.length" class="empty-state"><div><strong>还没有渠道类型</strong><span>添加 JSON 配置后即可在渠道表单中选用</span></div></div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="discoveryOpen" :title="`选择模型 · ${discoveryChannel?.name || ''}`" width="min(640px, 94vw)"><div v-loading="discovering" class="model-selection"><template v-if="discoveryError"><div class="discovery-error" role="alert"><strong>模型发现失败</strong><span>{{ discoveryError }}</span><el-button :icon="RefreshCw" @click="discoveryChannel && discover(discoveryChannel)">重新尝试</el-button></div></template><template v-else><div class="selection-toolbar"><el-input v-model="discoveryKeyword" clearable placeholder="搜索模型" /><el-button :disabled="!visibleDiscoveredModels.length" @click="toggleVisibleModels">{{ allVisibleSelected ? '取消当前结果' : '选择当前结果' }}</el-button></div><div class="selection-summary"><span>已选择 {{ selectedModelNames.length }} 个</span><span>共发现 {{ discoveredModels.length }} 个</span></div><el-checkbox-group v-if="visibleDiscoveredModels.length" v-model="selectedModelNames" class="model-check-list"><el-checkbox v-for="item in visibleDiscoveredModels" :key="item.name" :value="item.name"><code>{{ item.name }}</code></el-checkbox></el-checkbox-group><div v-else-if="!discovering" class="selection-empty">{{ discoveredModels.length ? '没有匹配模型' : '上游没有返回模型' }}</div></template></div><template #footer><el-button @click="discoveryOpen = false">取消</el-button><el-button type="primary" :loading="applyingSelection" :disabled="discovering || Boolean(discoveryError)" @click="saveModelSelection">确认选择</el-button></template></el-dialog>

    <ChannelModelTestDialog v-model="testOpen" :channel="testChannel" @changed="load" />

    <el-drawer v-model="drawerOpen" :title="title" :size="drawerSize"><el-form label-position="top"><div class="form-grid"><el-form-item label="渠道名称"><el-input v-model="form.name" placeholder="例如 OpenAI 主线路" /></el-form-item><el-form-item label="渠道类型"><el-select v-model="form.type" filterable placeholder="选择渠道类型"><el-option v-for="item in activeTypes" :key="item.id" :label="`${item.name} (${item.code})`" :value="item.code" /></el-select></el-form-item><el-form-item label="API 根地址"><el-input v-model="form.baseUrl" placeholder="https://api.openai.com/v1" /></el-form-item><el-form-item label="推理密钥"><el-input v-model="form.apiKey" type="password" show-password :placeholder="editingId ? '留空则保持不变' : 'sk-... 或上游密钥'" autocomplete="new-password" /></el-form-item><el-form-item v-if="usesManagementKey" label="上游管理密钥（可选）"><el-input v-model="form.managementKey" type="password" show-password :placeholder="editingId ? '留空则清除；不修改请勿聚焦' : '用于渠道类型声明的管理接口'" autocomplete="new-password" /></el-form-item><el-form-item label="组织 ID"><el-input v-model="form.organizationId" clearable /></el-form-item><el-form-item label="项目 ID"><el-input v-model="form.projectId" clearable /></el-form-item><el-form-item label="优先级"><el-input-number v-model="form.priority" :min="-999" :max="999" controls-position="right" /></el-form-item><el-form-item label="权重"><el-input-number v-model="form.weight" :min="1" :max="1000" controls-position="right" /></el-form-item><el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><el-form-item label="渠道分组"><el-select v-model="form.groupIds" multiple filterable clearable placeholder="不选择表示未分组"><el-option v-for="item in store.channelGroups" :key="item.id" :label="`${item.name} (${item.code})`" :value="item.id" /></el-select></el-form-item></el-form><template #footer><el-button @click="drawerOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存渠道</el-button></template></el-drawer>

    <el-drawer v-model="groupDrawerOpen" :title="editingGroup ? '编辑渠道分组' : '添加渠道分组'" :size="drawerSize"><el-form label-position="top"><div class="form-grid"><el-form-item label="分组名称"><el-input v-model="groupForm.name" placeholder="例如 高优先级" /></el-form-item><el-form-item label="分组代码"><el-input v-model="groupForm.code" :disabled="Boolean(editingGroup)" placeholder="例如 premium" /></el-form-item><el-form-item label="状态"><el-switch v-model="groupForm.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><el-form-item label="说明"><el-input v-model="groupForm.description" maxlength="255" show-word-limit /></el-form-item><el-form-item label="包含渠道"><el-select v-model="groupForm.channelIds" multiple filterable clearable placeholder="选择渠道"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select></el-form-item></el-form><template #footer><el-button @click="groupDrawerOpen = false">取消</el-button><el-button type="primary" :loading="groupSaving" @click="saveGroup">保存分组</el-button></template></el-drawer>

    <el-drawer v-model="typeDrawerOpen" :title="editingType ? '编辑渠道类型' : '添加渠道类型'" :size="typeDrawerSize"><el-form label-position="top"><div class="form-grid"><el-form-item label="类型名称"><el-input v-model="typeForm.name" placeholder="例如 自建网关" /></el-form-item><el-form-item label="类型代码"><el-input v-model="typeForm.code" :disabled="Boolean(editingType)" placeholder="例如 custom_gateway" /></el-form-item><el-form-item label="状态"><el-switch v-model="typeForm.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><el-form-item label="类型 JSON 配置"><el-input v-model="typeForm.configText" class="config-editor" type="textarea" :rows="22" spellcheck="false" /></el-form-item></el-form><template #footer><el-button @click="typeDrawerOpen = false">取消</el-button><el-button type="primary" :loading="typeSaving" @click="saveType">保存类型</el-button></template></el-drawer>
  </div>
</template>

<style scoped>
.channel-tabs :deep(.el-tabs__header) { margin: 0 0 18px; }.channel-tabs :deep(.el-tabs__nav-wrap::after) { background: #dce2e7; }.tab-label { display: inline-flex; align-items: center; gap: 7px; }.page-toolbar { display: flex; min-height: 36px; align-items: center; gap: 10px; margin-bottom: 18px; }.page-toolbar .spacer { flex: 1; }.channel-name, .type-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.channel-name strong, .type-cell strong { font-size: 13px; }.channel-name span, .type-cell code { overflow: hidden; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.cost-cell { display: flex; flex-direction: column; gap: 2px; font-size: 11px; }.cost-cell small, .table-panel small { color: #7b8792; }.group-channel-tag { margin: 0 4px 4px 0; }.model-selection { min-height: 300px; }.selection-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }.selection-summary { display: flex; justify-content: space-between; margin: 13px 0 8px; color: #66717d; font-size: 11px; }.model-check-list { display: grid; max-height: 390px; overflow-y: auto; border-block: 1px solid #dce2e7; }.model-check-list .el-checkbox { min-width: 0; height: 38px; margin: 0; padding: 0 10px; border-bottom: 1px solid #eef1f3; }.model-check-list .el-checkbox:last-child { border-bottom: 0; }.model-check-list code { overflow-wrap: anywhere; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.selection-empty { display: grid; min-height: 220px; place-items: center; color: #66717d; font-size: 12px; }.discovery-error { display: flex; min-height: 220px; flex-direction: column; align-items: flex-start; justify-content: center; gap: 12px; padding: 20px; border: 1px solid #e9abb2; border-radius: 6px; color: #9c2836; background: #fff6f7; font-size: 12px; line-height: 1.55; }.discovery-error strong { font-size: 14px; }.test-dialog { min-height: 180px; }.test-result { padding: 13px; border: 1px solid #dce2e7; border-radius: 6px; }.test-result.success { border-color: #acd7cc; background: #f2faf8; }.test-result.failed { border-color: #e9abb2; background: #fff6f7; }.test-result span { display: block; margin-top: 5px; color: #66717d; font-size: 11px; }.test-result p { margin: 8px 0 0; font-size: 12px; }.model-option { display: flex; align-items: center; justify-content: space-between; gap: 16px; }.model-option small { color: #7b8792; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.config-editor :deep(textarea) { min-height: 440px !important; font-family: 'JetBrains Mono', monospace; font-size: 12px; line-height: 1.55; }
@media (max-width: 600px) { .page-toolbar { align-items: flex-start; flex-wrap: wrap; }.page-toolbar .spacer { display: none; }.selection-toolbar { grid-template-columns: 1fr; }.model-option small { display: none; } }
</style>
