<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Braces, Coins, Eye, FlaskConical, KeyRound, Network, Pencil, Plus, RefreshCw, ScanSearch, Tags, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelInput, ChannelModel, DiscoveredModel } from '../api/types'
import ChannelAdvancedSettings from '../components/ChannelAdvancedSettings.vue'
import ChannelCredentialDrawer from '../components/ChannelCredentialDrawer.vue'
import ChannelModelTestDialog from '../components/ChannelModelTestDialog.vue'
import ChannelRouteCoverageSettings from '../components/ChannelRouteCoverageSettings.vue'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { formatCost, formatLatency, formatTime } from '../lib/format'
import { channelTypeBaseURL, createDefaultChannelAdvancedConfig, createEmptyChannelInput } from '../lib/channelForm'
import { sortDiscoveredModels } from '../lib/models'
import { type ChannelTab, useChannelConfiguration } from '../composables/useChannelConfiguration'

const store = useAppStore()

const activeTab = ref<ChannelTab>('channels')
const tabLoading = reactive<Record<ChannelTab, boolean>>({ channels: false, groups: false, types: false })
const tabLoaded = reactive<Record<ChannelTab, boolean>>({ channels: false, groups: false, types: false })
const saving = ref(false)
const drawerOpen = ref(false)
const channelFormLoading = ref(false)
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
const credentialsOpen = ref(false)
const credentialChannel = ref<Channel>()
const healthCheckModels = ref<ChannelModel[]>([])

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
const healthCheckModelOptions = computed(() => [...healthCheckModels.value]
  .filter((item) => item.enabled === 1)
  .sort((left, right) => left.publicName.localeCompare(right.publicName) || left.upstreamName.localeCompare(right.upstreamName)))

const form = reactive<ChannelInput>(createEmptyChannelInput())
const title = computed(() => editingId.value ? '编辑渠道' : '添加渠道')
const editingChannel = computed(() => store.channels.find((item) => item.id === editingId.value))

async function loadChannels() {
  tabLoading.channels = true
  try {
    await store.loadChannels()
    tabLoaded.channels = true
  } catch (error) { showError(error, '加载渠道失败') } finally { tabLoading.channels = false }
}

async function loadChannelGroups() {
  tabLoading.groups = true
  try {
    await store.loadChannelGroups()
    tabLoaded.groups = true
  } catch (error) { showError(error, '加载渠道分组失败') } finally { tabLoading.groups = false }
}

async function loadChannelTypes() {
  tabLoading.types = true
  try {
    await store.loadChannelTypes()
    tabLoaded.types = true
  } catch (error) { showError(error, '加载渠道类型失败') } finally { tabLoading.types = false }
}

function loadTab(tab: ChannelTab) {
  return { channels: loadChannels, groups: loadChannelGroups, types: loadChannelTypes }[tab]()
}

function handleTabChange(tab: string | number) {
  void loadTab(tab as ChannelTab)
}

async function ensureTabs(...tabs: ChannelTab[]) {
  await Promise.all(tabs.filter((tab) => !tabLoaded[tab]).map(loadTab))
}

async function loadChannelFormOptions() {
  channelFormLoading.value = true
  try { await ensureTabs('types', 'groups') } finally { channelFormLoading.value = false }
}

const {
  costLabel,
  editingGroup,
  editingType,
  groupDrawerOpen,
  groupForm,
  groupFormLoading,
  groupSaving,
  openCreateGroup,
  openCreateType,
  openEditGroup,
  openEditType,
  removeGroup,
  removeType,
  saveGroup,
  saveType,
  setTypeStatus,
  typeDrawerOpen,
  typeForm,
  typeReadOnly,
  typeSaving,
  typeStatusSaving,
} = useChannelConfiguration({ store, tabLoaded, ensureTabs, loadChannelGroups, loadChannelTypes })

async function openCreate() {
	editingId.value = undefined
	healthCheckModels.value = []
  Object.assign(form, createEmptyChannelInput())
  drawerOpen.value = true
  await loadChannelFormOptions()
  const firstType = store.channelTypes.find((item) => item.status === 1)
  form.type = firstType?.code || ''
  applyDefaultBaseURL(firstType?.code || '')
}

function applyDefaultBaseURL(type: string) {
  if (!editingId.value) form.baseUrl = channelTypeBaseURL(store.channelTypes, type) || form.baseUrl
}
async function openEdit(channel: Channel) {
  editingId.value = channel.id
  Object.assign(form, {
    name: channel.name, type: channel.type, baseUrl: channel.baseUrl, apiKey: '', managementKey: undefined, proxyUrl: undefined,
	organizationId: channel.organizationId, projectId: channel.projectId, status: channel.status,
	priority: channel.priority, weight: channel.weight, healthCheckModelId: channel.healthCheckModelId, autoDisableEnabled: channel.autoDisableEnabled, advancedConfig: JSON.parse(JSON.stringify(channel.advancedConfig || createDefaultChannelAdvancedConfig())), groupIds: channel.groupIds || [],
	})
	drawerOpen.value = true
	void loadHealthCheckModels(channel.id)
	await loadChannelFormOptions()
}

async function loadHealthCheckModels(channelID: number) {
	healthCheckModels.value = []
	try {
		healthCheckModels.value = await apiGet<ChannelModel[]>(`/channels/${channelID}/models`)
	} catch (error) {
		showError(error, '加载测试模型失败')
	}
}

function clearProxy() {
  form.proxyUrl = ''
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
    tabLoaded.groups = false
    await loadChannels()
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
    await loadChannels()
  } catch (error) { showError(error, '保存模型选择失败') } finally { applyingSelection.value = false }
}

async function openTest(channel: Channel) {
  testChannel.value = channel
  testOpen.value = true
}

function openCredentials(channel: Channel) {
  credentialChannel.value = channel
  credentialsOpen.value = true
}

function queryCost(channel: Channel) {
  openCredentials(channel)
}

async function remove(channel: Channel) {
  try {
    await ElMessageBox.confirm(`删除渠道“${channel.name}”？`, '删除渠道', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/channels/${channel.id}`)
    ElMessage.success('渠道已删除')
    tabLoaded.groups = false
    await loadChannels()
  } catch (error) {
    if (error !== 'cancel') showError(error, '删除渠道失败')
  }
}

onMounted(loadChannels)
</script>

<template>
  <div class="page-stack">
    <el-tabs v-model="activeTab" class="channel-tabs sticky-page-tabs" @tab-change="handleTabChange">
      <el-tab-pane name="channels">
        <template #label><span class="tab-label"><Network :size="15" />渠道</span></template>
        <div class="page-toolbar">
          <div class="muted">管理上游、模型选择、路由顺序和费用查询</div>
          <div class="spacer" />
          <el-button :icon="RefreshCw" :loading="tabLoading.channels" @click="loadChannels">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreate">添加渠道</el-button>
        </div>
        <div class="table-panel">
          <el-table v-loading="tabLoading.channels" :data="store.channels" row-key="id">
            <el-table-column label="渠道" min-width="180"><template #default="{ row }"><div class="channel-name"><strong>{{ row.name }}</strong><span>{{ row.baseUrl }}</span></div></template></el-table-column>
            <el-table-column label="类型" min-width="120"><template #default="{ row }"><div class="type-cell"><strong>{{ row.typeName }}</strong><code>{{ row.type }}</code></div></template></el-table-column>
            <el-table-column label="状态" min-width="154"><template #default="{ row }"><el-tooltip v-if="row.autoDisabled" :content="row.autoDisabledReason || '渠道被自动禁用'" placement="top"><span class="status-dot warning">渠道自动禁用</span></el-tooltip><el-tooltip v-else-if="row.credentialsUnavailable" content="该渠道当前不可路由：所有上游密钥均不可用，请在上游密钥中查看禁用详情。" placement="top"><span class="status-dot warning">所有密钥不可用</span></el-tooltip><span v-else class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '手动停用' }}</span></template></el-table-column>
            <el-table-column label="路由" width="108"><template #default="{ row }"><span class="mono">P{{ row.priority }} / W{{ row.weight }}</span></template></el-table-column>
            <el-table-column label="模型" width="100"><template #default="{ row }">{{ row.enabledModelCount }} / {{ row.discoveredModels }}</template></el-table-column>
            <el-table-column label="最近测试" min-width="130"><template #default="{ row }"><span v-if="row.lastTestStatus" class="status-dot" :class="row.lastTestStatus">{{ row.lastTestStatus === 'success' ? formatLatency(row.lastTestLatencyMs) : '失败' }}</span><span v-else class="muted">未测试</span></template></el-table-column>
            <el-table-column label="上游费用 / 余额" min-width="168"><template #default="{ row }"><button class="cost-link" type="button" @click="openCredentials(row)"><div v-if="row.costSummaries?.length" class="cost-cell"><template v-for="summary in row.costSummaries" :key="summary.currency"><span v-if="summary.usedAmount !== undefined">{{ summary.currency }} 已用 {{ formatCost(summary.usedAmount, summary.currency) }}</span><span v-if="summary.remainingAmount !== undefined">{{ summary.currency }} 余额 {{ formatCost(summary.remainingAmount, summary.currency) }}</span></template><small v-if="row.lastCostAt">{{ formatTime(row.lastCostAt) }}</small></div><span v-else class="muted">查看明细</span></button></template></el-table-column>
            <el-table-column label="操作" width="260" fixed="right" align="right"><template #default="{ row }"><div class="table-actions">
              <el-tooltip content="管理上游密钥"><button class="icon-button" type="button" :aria-label="`管理 ${row.name} 的上游密钥`" @click="openCredentials(row)"><KeyRound :size="16" /></button></el-tooltip>
              <el-tooltip content="发现模型"><button class="icon-button" type="button" :aria-label="`发现 ${row.name} 的模型`" @click="discover(row)"><ScanSearch :size="16" /></button></el-tooltip>
              <el-tooltip content="测试模型"><button class="icon-button" type="button" :aria-label="`测试 ${row.name} 的模型`" @click="openTest(row)"><FlaskConical :size="16" /></button></el-tooltip>
              <el-tooltip content="查询费用"><button class="icon-button" type="button" :aria-label="`查询 ${row.name} 的费用`" :disabled="row.costQueryMode === 'none'" @click="queryCost(row)"><Coins :size="16" /></button></el-tooltip>
              <el-tooltip content="编辑"><button class="icon-button" type="button" :aria-label="`编辑渠道 ${row.name}`" @click="openEdit(row)"><Pencil :size="16" /></button></el-tooltip>
              <el-tooltip content="删除"><button class="icon-button danger" type="button" :aria-label="`删除渠道 ${row.name}`" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip>
            </div></template></el-table-column>
          </el-table>
          <div v-if="!tabLoading.channels && !store.channels.length" class="empty-state"><div><strong>还没有渠道</strong><span>先添加渠道类型，再接入第一个上游</span></div></div>
        </div>
      </el-tab-pane>

      <el-tab-pane name="groups">
        <template #label><span class="tab-label"><Tags :size="15" />渠道分组</span></template>
        <div class="page-toolbar"><div class="muted">为密钥授权和路由策略维护渠道归属</div><div class="spacer" /><el-button :icon="RefreshCw" :loading="tabLoading.groups" @click="loadChannelGroups">刷新</el-button><el-button type="primary" :icon="Plus" @click="openCreateGroup">添加分组</el-button></div>
        <div class="table-panel">
          <el-table v-loading="tabLoading.groups" :data="store.channelGroups" row-key="id">
            <el-table-column label="分组" min-width="180"><template #default="{ row }"><div class="type-cell"><strong>{{ row.name }}</strong><code>{{ row.code }}</code></div></template></el-table-column>
            <el-table-column prop="description" label="说明" min-width="220" />
            <el-table-column label="渠道" min-width="180"><template #default="{ row }"><el-tag v-for="channelId in row.channelIds" :key="channelId" size="small" class="group-channel-tag">{{ store.channels.find(item => item.id === channelId)?.name || `#${channelId}` }}</el-tag><span v-if="!row.channelIds.length" class="muted">未分配</span></template></el-table-column>
            <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column>
            <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="编辑"><button class="icon-button" type="button" @click="openEditGroup(row)"><Pencil :size="16" /></button></el-tooltip><el-tooltip content="删除"><button class="icon-button danger" type="button" @click="removeGroup(row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
          </el-table>
          <div v-if="!tabLoading.groups && !store.channelGroups.length" class="empty-state"><div><strong>还没有渠道分组</strong><span>创建分组后可对访问密钥限定可用渠道</span></div></div>
        </div>
      </el-tab-pane>

      <el-tab-pane name="types">
        <template #label><span class="tab-label"><Braces :size="15" />渠道类型</span></template>
        <div class="page-toolbar">
          <div class="muted">JSON 定义模型发现、OpenAI 接口能力、鉴权和费用字段路径</div>
          <div class="spacer" />
          <el-button :icon="RefreshCw" :loading="tabLoading.types" @click="loadChannelTypes">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreateType">添加渠道类型</el-button>
        </div>
        <div class="table-panel">
          <el-table v-loading="tabLoading.types" :data="store.channelTypes" row-key="id">
            <el-table-column label="类型" min-width="170"><template #default="{ row }"><div class="type-cell"><strong>{{ row.name }}</strong><code>{{ row.code }}</code></div></template></el-table-column>
            <el-table-column label="模型接口" min-width="220"><template #default="{ row }"><span class="mono">{{ row.config.models.method }} {{ row.config.models.path }}</span></template></el-table-column>
            <el-table-column label="余额查询" min-width="160"><template #default="{ row }"><span>{{ costLabel(row) }}</span><small v-if="row.config.costs.path"> · {{ row.config.costs.path }}</small></template></el-table-column>
            <el-table-column label="状态" width="142"><template #default="{ row }"><div class="type-status-control"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span><el-switch :model-value="row.status === 1" :disabled="row.builtIn === 1 || typeStatusSaving[row.id]" :aria-label="`${row.name} ${row.status === 1 ? '已启用' : '已停用'}`" @update:model-value="setTypeStatus(row, $event)" /></div></template></el-table-column>
            <el-table-column label="来源" width="88"><template #default="{ row }"><span class="muted">{{ row.builtIn === 1 ? '内置' : '自定义' }}</span></template></el-table-column>
            <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip :content="row.builtIn === 1 ? '查看内置配置' : '编辑类型'"><button class="icon-button" type="button" :aria-label="`${row.builtIn === 1 ? '查看' : '编辑'}渠道类型 ${row.name}`" @click="openEditType(row)"><Eye v-if="row.builtIn === 1" :size="16" /><Pencil v-else :size="16" /></button></el-tooltip><el-tooltip v-if="row.builtIn === 0" content="删除类型"><button class="icon-button danger" type="button" :aria-label="`删除渠道类型 ${row.name}`" @click="removeType(row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
          </el-table>
          <div v-if="!tabLoading.types && !store.channelTypes.length" class="empty-state"><div><strong>还没有渠道类型</strong><span>添加 JSON 配置后即可在渠道表单中选用</span></div></div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="discoveryOpen" :title="`选择模型 · ${discoveryChannel?.name || ''}`" width="min(640px, 94vw)"><div v-loading="discovering" class="model-selection"><template v-if="discoveryError"><div class="discovery-error" role="alert"><strong>模型发现失败</strong><span>{{ discoveryError }}</span><el-button :icon="RefreshCw" @click="discoveryChannel && discover(discoveryChannel)">重新尝试</el-button></div></template><template v-else><div class="selection-toolbar"><el-input v-model="discoveryKeyword" clearable placeholder="搜索模型" /><el-button :disabled="!visibleDiscoveredModels.length" @click="toggleVisibleModels">{{ allVisibleSelected ? '取消当前结果' : '选择当前结果' }}</el-button></div><div class="selection-summary"><span>已选择 {{ selectedModelNames.length }} 个</span><span>共发现 {{ discoveredModels.length }} 个</span></div><el-checkbox-group v-if="visibleDiscoveredModels.length" v-model="selectedModelNames" class="model-check-list"><el-checkbox v-for="item in visibleDiscoveredModels" :key="item.name" :value="item.name"><code>{{ item.name }}</code></el-checkbox></el-checkbox-group><div v-else-if="!discovering" class="selection-empty">{{ discoveredModels.length ? '没有匹配模型' : '上游没有返回模型' }}</div></template></div><template #footer><el-button @click="discoveryOpen = false">取消</el-button><el-button type="primary" :loading="applyingSelection" :disabled="discovering || Boolean(discoveryError)" @click="saveModelSelection">确认选择</el-button></template></el-dialog>

    <ChannelModelTestDialog v-model="testOpen" :channel="testChannel" @changed="loadChannels" />
    <ChannelCredentialDrawer v-model="credentialsOpen" :channel="credentialChannel" @changed="loadChannels" />

    <el-drawer v-model="drawerOpen" :title="title" :size="drawerSize"><el-form v-loading="channelFormLoading" label-position="top"><div class="form-grid"><el-form-item label="渠道名称"><el-input v-model="form.name" placeholder="例如 OpenAI 主线路" /></el-form-item><el-form-item label="渠道类型"><el-select v-model="form.type" filterable placeholder="选择渠道类型" @change="applyDefaultBaseURL"><el-option v-for="item in activeTypes" :key="item.id" :label="`${item.name} (${item.code})`" :value="item.code" /></el-select></el-form-item><el-form-item label="API 根地址"><el-input v-model="form.baseUrl" :placeholder="selectedChannelType?.config.baseUrl || 'https://api.openai.com/v1'" /></el-form-item><el-form-item v-if="!editingId" label="首个推理密钥"><el-input v-model="form.apiKey" type="password" show-password placeholder="sk-... 或上游密钥" autocomplete="new-password" /></el-form-item><el-form-item v-if="usesManagementKey" label="上游管理密钥（可选）"><el-input v-model="form.managementKey" type="password" show-password :placeholder="editingId ? '留空则清除；不修改请勿聚焦' : '用于渠道类型声明的管理接口'" autocomplete="new-password" /></el-form-item><el-form-item label="组织 ID"><el-input v-model="form.organizationId" clearable /></el-form-item><el-form-item label="项目 ID"><el-input v-model="form.projectId" clearable /></el-form-item><el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><ChannelRouteCoverageSettings v-model:priority="form.priority" v-model:weight="form.weight" v-model:health-check-model-id="form.healthCheckModelId" v-model:auto-disable-enabled="form.autoDisableEnabled" :editing="Boolean(editingId)" :models="healthCheckModelOptions" /><el-form-item label="渠道分组"><el-select v-model="form.groupIds" multiple filterable clearable placeholder="不选择表示未分组"><el-option v-for="item in store.channelGroups" :key="item.id" :label="`${item.name} (${item.code})`" :value="item.id" /></el-select></el-form-item><ChannelAdvancedSettings v-model:config="form.advancedConfig" v-model:proxy-url="form.proxyUrl" :editing="Boolean(editingId)" :has-proxy="editingChannel?.hasProxy === true" @clear-proxy="clearProxy" /></el-form><template #footer><el-button @click="drawerOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存渠道</el-button></template></el-drawer>

    <el-drawer v-model="groupDrawerOpen" :title="editingGroup ? '编辑渠道分组' : '添加渠道分组'" :size="drawerSize"><el-form v-loading="groupFormLoading" label-position="top"><div class="form-grid"><el-form-item label="分组名称"><el-input v-model="groupForm.name" placeholder="例如 高优先级" /></el-form-item><el-form-item label="分组代码"><el-input v-model="groupForm.code" :disabled="Boolean(editingGroup)" placeholder="例如 premium" /></el-form-item><el-form-item label="状态"><el-switch v-model="groupForm.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><el-form-item label="说明"><el-input v-model="groupForm.description" maxlength="255" show-word-limit /></el-form-item><el-form-item label="包含渠道"><el-select v-model="groupForm.channelIds" multiple filterable clearable placeholder="选择渠道"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select></el-form-item></el-form><template #footer><el-button @click="groupDrawerOpen = false">取消</el-button><el-button type="primary" :loading="groupSaving" @click="saveGroup">保存分组</el-button></template></el-drawer>

    <el-drawer v-model="typeDrawerOpen" :title="typeReadOnly ? '查看内置渠道类型' : (editingType ? '编辑渠道类型' : '添加渠道类型')" :size="typeDrawerSize"><el-form label-position="top"><div class="form-grid"><el-form-item label="类型名称"><el-input v-model="typeForm.name" :readonly="typeReadOnly" placeholder="例如 自建网关" /></el-form-item><el-form-item label="类型代码"><el-input v-model="typeForm.code" :disabled="Boolean(editingType)" placeholder="例如 custom_gateway" /></el-form-item></div><el-form-item label="类型 JSON 配置"><el-input v-model="typeForm.configText" class="config-editor" type="textarea" :rows="22" :readonly="typeReadOnly" spellcheck="false" /></el-form-item></el-form><template #footer><el-button @click="typeDrawerOpen = false">{{ typeReadOnly ? '关闭' : '取消' }}</el-button><el-button v-if="!typeReadOnly" type="primary" :loading="typeSaving" @click="saveType">保存类型</el-button></template></el-drawer>
  </div>
</template>

<style scoped>
.channel-tabs :deep(.el-tabs__header) { margin: 0 0 18px; }.channel-tabs :deep(.el-tabs__nav-wrap::after) { background: #dce2e7; }.tab-label { display: inline-flex; align-items: center; gap: 7px; }.page-toolbar { display: flex; min-height: 36px; align-items: center; gap: 10px; margin-bottom: 18px; }.page-toolbar .spacer { flex: 1; }.channel-name, .type-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.channel-name strong, .type-cell strong { font-size: 13px; }.channel-name span, .type-cell code { overflow: hidden; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.type-status-control { display: inline-flex; align-items: center; gap: 10px; white-space: nowrap; }.type-status-control :deep(.el-switch) { flex: 0 0 auto; }.cost-cell { display: flex; flex-direction: column; gap: 2px; font-size: 11px; }.cost-cell small, .table-panel small { color: #7b8792; }.group-channel-tag { margin: 0 4px 4px 0; }.model-selection { min-height: 300px; }.selection-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }.selection-summary { display: flex; justify-content: space-between; margin: 13px 0 8px; color: #66717d; font-size: 11px; }.model-check-list { display: grid; max-height: 390px; overflow-y: auto; border-block: 1px solid #dce2e7; }.model-check-list .el-checkbox { min-width: 0; height: 38px; margin: 0; padding: 0 10px; border-bottom: 1px solid #eef1f3; }.model-check-list .el-checkbox:last-child { border-bottom: 0; }.model-check-list code { overflow-wrap: anywhere; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.selection-empty { display: grid; min-height: 220px; place-items: center; color: #66717d; font-size: 12px; }.discovery-error { display: flex; min-height: 220px; flex-direction: column; align-items: flex-start; justify-content: center; gap: 12px; padding: 20px; border: 1px solid #e9abb2; border-radius: 6px; color: #9c2836; background: #fff6f7; font-size: 12px; line-height: 1.55; }.discovery-error strong { font-size: 14px; }.test-dialog { min-height: 180px; }.test-result { padding: 13px; border: 1px solid #dce2e7; border-radius: 6px; }.test-result.success { border-color: #acd7cc; background: #f2faf8; }.test-result.failed { border-color: #e9abb2; background: #fff6f7; }.test-result span { display: block; margin-top: 5px; color: #66717d; font-size: 11px; }.test-result p { margin: 8px 0 0; font-size: 12px; }.model-option { display: flex; align-items: center; justify-content: space-between; gap: 16px; }.model-option small { color: #7b8792; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.config-editor :deep(textarea) { min-height: 440px !important; font-family: 'JetBrains Mono', monospace; font-size: 12px; line-height: 1.55; }
.cost-link { display: block; width: 100%; border: 0; padding: 0; text-align: left; color: inherit; background: transparent; cursor: pointer; }.cost-link:hover span { color: #1677ff; }
@media (max-width: 600px) { .page-toolbar { align-items: flex-start; flex-wrap: wrap; }.page-toolbar .spacer { display: none; }.selection-toolbar { grid-template-columns: 1fr; }.model-option small { display: none; } }
</style>
