import { computed, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { ChannelGroup, ChannelType, ChannelTypeConfig } from '../api/types'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'

export type ChannelTab = 'channels' | 'groups' | 'types'

type Options = {
  store: ReturnType<typeof useAppStore>
  tabLoaded: Record<ChannelTab, boolean>
  ensureTabs: (...tabs: ChannelTab[]) => Promise<void>
  loadChannelGroups: () => Promise<void>
  loadChannelTypes: () => Promise<void>
}

export function useChannelConfiguration(options: Options) {
  const typeDrawerOpen = ref(false)
  const typeSaving = ref(false)
  const editingType = ref<ChannelType>()
  const typeStatusSaving = reactive<Record<number, boolean>>({})
  const typeForm = reactive({ name: '', code: '', configText: '' })
  const groupDrawerOpen = ref(false)
  const groupSaving = ref(false)
  const groupFormLoading = ref(false)
  const editingGroup = ref<ChannelGroup>()
  const groupForm = reactive({ name: '', code: '', description: '', status: 1, channelIds: [] as number[] })
  const typeReadOnly = computed(() => editingType.value?.builtIn === 1)

  async function loadGroupFormChannels() {
    groupFormLoading.value = true
    try {
      await options.ensureTabs('channels')
    } finally {
      groupFormLoading.value = false
    }
  }

  async function openCreateType() {
    editingType.value = undefined
    Object.assign(typeForm, { name: '', code: '', configText: '' })
    typeDrawerOpen.value = true
    try {
      typeForm.configText = JSON.stringify(await apiGet<ChannelTypeConfig>('/channel-types/default-config'), null, 2)
    } catch (error) {
      showError(error, '加载 OpenAI 默认配置失败')
    }
  }

  function openEditType(item: ChannelType) {
    editingType.value = item
    Object.assign(typeForm, { name: item.name, code: item.code, configText: JSON.stringify(item.config, null, 2) })
    typeDrawerOpen.value = true
  }

  async function saveType() {
    let config: ChannelTypeConfig | undefined
    if (typeForm.configText.trim()) {
      try {
        config = JSON.parse(typeForm.configText)
      } catch {
        showError('渠道类型 JSON 格式无效', '格式错误')
        return
      }
    }
    if (!typeForm.name.trim() || !typeForm.code.trim()) {
      showError('请填写类型名称和类型代码', '信息不完整')
      return
    }
    typeSaving.value = true
    try {
      const payload = { name: typeForm.name, code: typeForm.code, ...(config ? { config } : {}) }
      if (editingType.value) await apiPut(`/channel-types/${editingType.value.id}`, payload)
      else await apiPost('/channel-types', payload)
      ElMessage.success(editingType.value ? '渠道类型已更新' : '渠道类型已添加')
      typeDrawerOpen.value = false
      await options.loadChannelTypes()
    } catch (error) {
      showError(error, '保存渠道类型失败')
    } finally {
      typeSaving.value = false
    }
  }

  async function setTypeStatus(item: ChannelType, enabled: boolean) {
    typeStatusSaving[item.id] = true
    try {
      await apiPut(`/channel-types/${item.id}/status`, { status: enabled ? 1 : 0 })
      item.status = enabled ? 1 : 0
      ElMessage.success(enabled ? '渠道类型已启用' : '渠道类型已停用')
    } catch (error) {
      showError(error, '更新渠道类型状态失败')
    } finally {
      typeStatusSaving[item.id] = false
    }
  }

  async function removeType(item: ChannelType) {
    try {
      await ElMessageBox.confirm(`删除渠道类型“${item.name}”？`, '删除渠道类型', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
      await apiDelete(`/channel-types/${item.id}`)
      ElMessage.success('渠道类型已删除')
      options.tabLoaded.channels = false
      await options.loadChannelTypes()
    } catch (error) {
      if (error !== 'cancel') showError(error, '删除渠道类型失败')
    }
  }

  function costLabel(item: ChannelType) {
    return { none: '不查询', openai_costs: 'OpenAI Costs', sub2api_usage: 'Sub2API Usage', newapi_balance: 'NewAPI 余额', custom_json: '自定义 JSON' }[item.config.costs.adapter] || item.config.costs.adapter
  }

  async function openCreateGroup() {
    editingGroup.value = undefined
    Object.assign(groupForm, { name: '', code: '', description: '', status: 1, channelIds: [] })
    groupDrawerOpen.value = true
    await loadGroupFormChannels()
  }

  async function openEditGroup(item: ChannelGroup) {
    editingGroup.value = item
    Object.assign(groupForm, { name: item.name, code: item.code, description: item.description, status: item.status, channelIds: [...item.channelIds] })
    groupDrawerOpen.value = true
    await loadGroupFormChannels()
  }

  async function saveGroup() {
    if (!groupForm.name.trim() || !groupForm.code.trim()) {
      showError('请填写分组名称和代码', '信息不完整')
      return
    }
    groupSaving.value = true
    try {
      const payload = { ...groupForm }
      if (editingGroup.value) await apiPut(`/channel-groups/${editingGroup.value.id}`, payload)
      else await apiPost('/channel-groups', payload)
      ElMessage.success(editingGroup.value ? '渠道分组已更新' : '渠道分组已添加')
      groupDrawerOpen.value = false
      await options.loadChannelGroups()
    } catch (error) {
      showError(error, '保存渠道分组失败')
    } finally {
      groupSaving.value = false
    }
  }

  async function removeGroup(item: ChannelGroup) {
    try {
      await ElMessageBox.confirm(`删除渠道分组“${item.name}”？`, '删除渠道分组', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
      await apiDelete(`/channel-groups/${item.id}`)
      ElMessage.success('渠道分组已删除')
      await options.loadChannelGroups()
    } catch (error) {
      if (error !== 'cancel') showError(error, '删除渠道分组失败')
    }
  }

  return {
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
  }
}
