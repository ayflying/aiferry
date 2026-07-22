<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { RefreshCw } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelCostResult, ChannelInput, ChannelModel, DiscoveredModel } from '../api/types'
import ChannelAdvancedSettings from '../components/ChannelAdvancedSettings.vue'
import ChannelCredentialDrawer from '../components/ChannelCredentialDrawer.vue'
import ChannelGroupListPanel from '../components/ChannelGroupListPanel.vue'
import ChannelListPanel from '../components/ChannelListPanel.vue'
import ChannelModelTestDialog from '../components/ChannelModelTestDialog.vue'
import ChannelRouteCoverageSettings from '../components/ChannelRouteCoverageSettings.vue'
import ChannelTypeListPanel from '../components/ChannelTypeListPanel.vue'
import { type ChannelTab, useChannelConfiguration } from '../composables/useChannelConfiguration'
import { channelTypeBaseURL, createDefaultChannelAdvancedConfig, createEmptyChannelInput } from '../lib/channelForm'
import { showError } from '../lib/error'
import { sortDiscoveredModels } from '../lib/models'
import { useAppStore } from '../stores/app'

const store = useAppStore()
const route = useRoute()

const activeTab = computed<ChannelTab>(() => {
  const tab = route.meta.channelTab
  return tab === 'groups' || tab === 'types' ? tab : 'channels'
})
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
const queryingCostID = ref<number>()
const channelStatusSaving = ref<Record<number, boolean>>({})

const drawerSize = window.innerWidth <= 600 ? '94%' : '620px'
const typeDrawerSize = window.innerWidth <= 600 ? '94%' : '680px'
const form = reactive<ChannelInput>(createEmptyChannelInput())
const title = computed(() => editingId.value ? '编辑渠道' : '添加渠道')
const editingChannel = computed(() => store.channels.find((item) => item.id === editingId.value))
const activeTypes = computed(() => store.channelTypes.filter((item) => item.status === 1 || item.code === form.type))
const selectedChannelType = computed(() => store.channelTypes.find((item) => item.code === form.type))
const usesManagementKey = computed(() => {
  const config = selectedChannelType.value?.config
  return Boolean(config && [config.models.authType, config.costs.authType, config.pricing.authType].includes('management_key'))
})
const visibleDiscoveredModels = computed(() => {
  const keyword = discoveryKeyword.value.trim().toLowerCase()
  return keyword ? discoveredModels.value.filter((item) => item.name.toLowerCase().includes(keyword)) : discoveredModels.value
})
const allVisibleSelected = computed(() => visibleDiscoveredModels.value.length > 0
  && visibleDiscoveredModels.value.every((item) => selectedModelNames.value.includes(item.name)))
const healthCheckModelOptions = computed(() => [...healthCheckModels.value]
  .filter((item) => item.enabled === 1)
  .sort((left, right) => left.publicName.localeCompare(right.publicName) || left.upstreamName.localeCompare(right.upstreamName)))

async function loadChannels() {
  tabLoading.channels = true
  try {
    await store.loadChannels()
    tabLoaded.channels = true
  } catch (error) {
    showError(error, '加载渠道失败')
  } finally {
    tabLoading.channels = false
  }
}

async function loadChannelGroups() {
  tabLoading.groups = true
  try {
    await store.loadChannelGroups()
    tabLoaded.groups = true
  } catch (error) {
    showError(error, '加载渠道分组失败')
  } finally {
    tabLoading.groups = false
  }
}

async function loadChannelTypes() {
  tabLoading.types = true
  try {
    await store.loadChannelTypes()
    tabLoaded.types = true
  } catch (error) {
    showError(error, '加载渠道类型失败')
  } finally {
    tabLoading.types = false
  }
}

function loadTab(tab: ChannelTab) {
  return { channels: loadChannels, groups: loadChannelGroups, types: loadChannelTypes }[tab]()
}

async function ensureTabs(...tabs: ChannelTab[]) {
  await Promise.all(tabs.filter((tab) => !tabLoaded[tab]).map(loadTab))
}

async function loadChannelFormOptions() {
  channelFormLoading.value = true
  try {
    await ensureTabs('types', 'groups')
  } finally {
    channelFormLoading.value = false
  }
}

const {
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
  if (!editingId.value) form.baseUrl = channelTypeBaseURL(store.channelTypes, type)
}

async function openEdit(channel: Channel) {
  editingId.value = channel.id
  Object.assign(form, {
    name: channel.name,
    type: channel.type,
    baseUrl: channel.baseUrl,
    apiKey: '',
    managementKey: undefined,
    proxyUrl: undefined,
    organizationId: channel.organizationId,
    projectId: channel.projectId,
    status: channel.status,
    priority: channel.priority,
    weight: channel.weight,
    healthCheckModelId: channel.healthCheckModelId,
    autoDisableEnabled: channel.autoDisableEnabled,
    advancedConfig: JSON.parse(JSON.stringify(channel.advancedConfig || createDefaultChannelAdvancedConfig())),
    groupIds: channel.groupIds || [],
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
  } catch (error) {
    showError(error, '保存渠道失败')
  } finally {
    saving.value = false
  }
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
  } finally {
    discovering.value = false
  }
}

function describeDiscoveryError(error: unknown) {
  const message = error instanceof Error ? error.message : '网络请求失败'
  if (message.startsWith('上游每日用量额度已用尽')) return message
  if (message.includes('HTTP 429')) return '上游返回 HTTP 429，当前请求受到限流或该密钥的可用配额不足。请稍后重试，或在上游确认配额和请求限制。'
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
  } catch (error) {
    showError(error, '保存模型选择失败')
  } finally {
    applyingSelection.value = false
  }
}

function openTest(channel: Channel) {
  testChannel.value = channel
  testOpen.value = true
}

function openCredentials(channel: Channel) {
  credentialChannel.value = channel
  credentialsOpen.value = true
}

async function queryCost(channel: Channel) {
  if (queryingCostID.value) return
  queryingCostID.value = channel.id
  try {
    const result = await apiPost<ChannelCostResult>(`/channels/${channel.id}/costs/query`, {})
    const failures = result.credentials.filter((item) => item.error).length
    ElMessage.success(failures ? `费用已更新，${failures} 个上游密钥查询失败` : '费用已更新')
    await loadChannels()
  } catch (error) {
    showError(error, '查询上游费用失败')
  } finally {
    queryingCostID.value = undefined
  }
}

async function setChannelStatus(channel: Channel, enabled: boolean) {
  if (channelStatusSaving.value[channel.id]) return
  channelStatusSaving.value[channel.id] = true
  try {
    await apiPut(`/channels/${channel.id}/status`, { status: enabled ? 1 : 0 })
    ElMessage.success(enabled ? '渠道已启用，全部上游密钥已恢复可用' : '渠道已手动停用')
    await loadChannels()
  } catch (error) {
    showError(error, enabled ? '启用渠道失败' : '停用渠道失败')
  } finally {
    delete channelStatusSaving.value[channel.id]
  }
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

watch(activeTab, (tab) => {
  void ensureTabs(tab === 'groups' ? 'channels' : tab, tab)
}, { immediate: true })
</script>

<template>
  <div class="page-stack">
    <section v-if="activeTab === 'channels'"><ChannelListPanel :channels="store.channels" :loading="tabLoading.channels" :querying-cost-i-d="queryingCostID" :status-saving="channelStatusSaving" @create="openCreate" @discover="discover" @edit="openEdit" @open-credentials="openCredentials" @query-cost="queryCost" @refresh="loadChannels" @remove="remove" @set-status="setChannelStatus" @test="openTest" /></section>
    <section v-else-if="activeTab === 'groups'"><ChannelGroupListPanel :channels="store.channels" :groups="store.channelGroups" :loading="tabLoading.groups" @create="openCreateGroup" @edit="openEditGroup" @refresh="loadChannelGroups" @remove="removeGroup" /></section>
    <section v-else><ChannelTypeListPanel :loading="tabLoading.types" :status-saving="typeStatusSaving" :types="store.channelTypes" @create="openCreateType" @edit="openEditType" @refresh="loadChannelTypes" @remove="removeType" @set-status="setTypeStatus" /></section>

    <el-dialog v-model="discoveryOpen" :title="`选择模型 · ${discoveryChannel?.name || ''}`" width="min(640px, 94vw)"><div v-loading="discovering" class="model-selection"><template v-if="discoveryError"><div class="discovery-error" role="alert"><strong>模型发现失败</strong><span>{{ discoveryError }}</span><el-button :icon="RefreshCw" @click="discoveryChannel && discover(discoveryChannel)">重新尝试</el-button></div></template><template v-else><div class="selection-toolbar"><el-input v-model="discoveryKeyword" clearable placeholder="搜索模型" /><el-button :disabled="!visibleDiscoveredModels.length" @click="toggleVisibleModels">{{ allVisibleSelected ? '取消当前结果' : '选择当前结果' }}</el-button></div><div class="selection-summary"><span>已选择 {{ selectedModelNames.length }} 个</span><span>共发现 {{ discoveredModels.length }} 个</span></div><el-checkbox-group v-if="visibleDiscoveredModels.length" v-model="selectedModelNames" class="model-check-list"><el-checkbox v-for="item in visibleDiscoveredModels" :key="item.name" :value="item.name"><code>{{ item.name }}</code></el-checkbox></el-checkbox-group><div v-else-if="!discovering" class="selection-empty">{{ discoveredModels.length ? '没有匹配模型' : '上游没有返回模型' }}</div></template></div><template #footer><el-button @click="discoveryOpen = false">取消</el-button><el-button type="primary" :loading="applyingSelection" :disabled="discovering || Boolean(discoveryError)" @click="saveModelSelection">确认选择</el-button></template></el-dialog>

    <ChannelModelTestDialog v-model="testOpen" :channel="testChannel" @changed="loadChannels" />
    <ChannelCredentialDrawer v-model="credentialsOpen" :channel="credentialChannel" @changed="loadChannels" />

    <el-drawer v-model="drawerOpen" :title="title" :size="drawerSize"><el-form v-loading="channelFormLoading" label-position="top"><div class="form-grid"><el-form-item label="渠道名称"><el-input v-model="form.name" placeholder="例如 OpenAI 主线路" /></el-form-item><el-form-item label="渠道类型"><el-select v-model="form.type" filterable placeholder="选择渠道类型" @change="applyDefaultBaseURL"><el-option v-for="item in activeTypes" :key="item.id" :label="`${item.name} (${item.code})`" :value="item.code" /></el-select></el-form-item><el-form-item label="API 根地址"><el-input v-model="form.baseUrl" :placeholder="selectedChannelType?.config.baseUrl || 'https://api.openai.com/v1'" /></el-form-item><el-form-item v-if="!editingId" label="首个推理密钥"><el-input v-model="form.apiKey" type="password" show-password placeholder="sk-... 或上游密钥" autocomplete="new-password" /></el-form-item><el-form-item v-if="usesManagementKey" label="上游管理密钥（可选）"><el-input v-model="form.managementKey" type="password" show-password :placeholder="editingId ? '留空则清除；不修改请勿聚焦' : '用于渠道类型声明的管理接口'" autocomplete="new-password" /></el-form-item><el-form-item label="组织 ID"><el-input v-model="form.organizationId" clearable /></el-form-item><el-form-item label="项目 ID"><el-input v-model="form.projectId" clearable /></el-form-item><el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><ChannelRouteCoverageSettings v-model:priority="form.priority" v-model:weight="form.weight" v-model:health-check-model-id="form.healthCheckModelId" v-model:auto-disable-enabled="form.autoDisableEnabled" :editing="Boolean(editingId)" :models="healthCheckModelOptions" /><el-form-item label="渠道分组"><el-select v-model="form.groupIds" multiple filterable clearable placeholder="不选择表示未分组"><el-option v-for="item in store.channelGroups" :key="item.id" :label="`${item.name} (${item.code})`" :value="item.id" /></el-select></el-form-item><ChannelAdvancedSettings v-model:config="form.advancedConfig" v-model:proxy-url="form.proxyUrl" :editing="Boolean(editingId)" :has-proxy="editingChannel?.hasProxy === true" @clear-proxy="clearProxy" /></el-form><template #footer><el-button @click="drawerOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存渠道</el-button></template></el-drawer>

    <el-drawer v-model="groupDrawerOpen" :title="editingGroup ? '编辑渠道分组' : '添加渠道分组'" :size="drawerSize"><el-form v-loading="groupFormLoading" label-position="top"><div class="form-grid"><el-form-item label="分组名称"><el-input v-model="groupForm.name" placeholder="例如 高优先级" /></el-form-item><el-form-item label="分组代码"><el-input v-model="groupForm.code" :disabled="Boolean(editingGroup)" placeholder="例如 premium" /></el-form-item><el-form-item label="状态"><el-switch v-model="groupForm.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><el-form-item label="说明"><el-input v-model="groupForm.description" maxlength="255" show-word-limit /></el-form-item><el-form-item label="包含渠道"><el-select v-model="groupForm.channelIds" multiple filterable clearable placeholder="选择渠道"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select></el-form-item></el-form><template #footer><el-button @click="groupDrawerOpen = false">取消</el-button><el-button type="primary" :loading="groupSaving" @click="saveGroup">保存分组</el-button></template></el-drawer>

    <el-drawer v-model="typeDrawerOpen" :title="typeReadOnly ? '查看内置渠道类型' : (editingType ? '编辑渠道类型' : '添加渠道类型')" :size="typeDrawerSize"><el-form label-position="top"><div class="form-grid"><el-form-item label="类型名称"><el-input v-model="typeForm.name" :readonly="typeReadOnly" placeholder="例如 自建网关" /></el-form-item><el-form-item label="类型代码"><el-input v-model="typeForm.code" :disabled="Boolean(editingType)" placeholder="例如 custom_gateway" /></el-form-item></div><el-form-item label="类型 JSON 配置"><el-input v-model="typeForm.configText" class="config-editor" type="textarea" :rows="22" :readonly="typeReadOnly" spellcheck="false" /></el-form-item></el-form><template #footer><el-button @click="typeDrawerOpen = false">{{ typeReadOnly ? '关闭' : '取消' }}</el-button><el-button v-if="!typeReadOnly" type="primary" :loading="typeSaving" @click="saveType">保存类型</el-button></template></el-drawer>
  </div>
</template>

<style scoped>
.model-selection { min-height: 300px; }.selection-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }.selection-summary { display: flex; justify-content: space-between; margin: 13px 0 8px; color: #66717d; font-size: 11px; }.model-check-list { display: grid; max-height: 390px; overflow-y: auto; border-block: 1px solid #dce2e7; }.model-check-list .el-checkbox { min-width: 0; height: 38px; margin: 0; padding: 0 10px; border-bottom: 1px solid #eef1f3; }.model-check-list .el-checkbox:last-child { border-bottom: 0; }.model-check-list code { overflow-wrap: anywhere; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.selection-empty { display: grid; min-height: 220px; place-items: center; color: #66717d; font-size: 12px; }.discovery-error { display: flex; min-height: 220px; flex-direction: column; align-items: flex-start; justify-content: center; gap: 12px; padding: 20px; border: 1px solid #e9abb2; border-radius: 6px; color: #9c2836; background: #fff6f7; font-size: 12px; line-height: 1.55; }.discovery-error strong { font-size: 14px; }.config-editor :deep(textarea) { min-height: 440px !important; font-family: 'JetBrains Mono', monospace; font-size: 12px; line-height: 1.55; }
@media (max-width: 600px) { .selection-toolbar { grid-template-columns: 1fr; } }
</style>
