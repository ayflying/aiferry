<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Plus, RefreshCw, Trash2 } from '@lucide/vue'

import type { DiscoveredModel } from '../api/types'

type ModelMappingRow = { id: number; upstreamName: string; publicName: string }

const props = defineProps<{
  modelValue: boolean
  channelName: string
  discovering: boolean
  discoveryError: string
  applying: boolean
  discoveredModels: DiscoveredModel[]
  selectedModelNames: string[]
  discoveryKeyword: string
  modelMappings: ModelMappingRow[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'update:selectedModelNames': [value: string[]]
  'update:discoveryKeyword': [value: string]
  'update:modelMappings': [value: ModelMappingRow[]]
  retry: []
  addMapping: []
  removeMapping: [id: number]
  save: []
}>()

const activeTab = ref<'selection' | 'mapping'>('selection')
const visibleDiscoveredModels = computed(() => {
  const keyword = props.discoveryKeyword.trim().toLowerCase()
  return keyword
    ? props.discoveredModels.filter((item) => item.name.toLowerCase().includes(keyword))
    : props.discoveredModels
})
const allVisibleSelected = computed(() => visibleDiscoveredModels.value.length > 0
  && visibleDiscoveredModels.value.every((item) => props.selectedModelNames.includes(item.name)))

watch(() => props.modelValue, (visible) => {
  if (visible) activeTab.value = 'selection'
})

function toggleVisibleModels() {
  const selected = new Set(props.selectedModelNames)
  for (const model of visibleDiscoveredModels.value) {
    if (allVisibleSelected.value) selected.delete(model.name)
    else selected.add(model.name)
  }
  emit('update:selectedModelNames', [...selected])
}

function updateMapping(id: number, field: 'upstreamName' | 'publicName', value: string) {
  emit('update:modelMappings', props.modelMappings.map((item) => item.id === id ? { ...item, [field]: value } : item))
}
</script>

<template>
  <el-dialog :model-value="modelValue" :title="`模型映射 · ${channelName}`" width="min(760px, 94vw)" @update:model-value="emit('update:modelValue', $event)">
    <div v-loading="discovering" class="model-selection">
      <template v-if="discoveryError">
        <div class="discovery-error" role="alert"><strong>模型发现失败</strong><span>{{ discoveryError }}</span><el-button :icon="RefreshCw" @click="emit('retry')">重新尝试</el-button></div>
      </template>
      <el-tabs v-else v-model="activeTab" class="model-tabs">
        <el-tab-pane label="选择模型" name="selection">
          <div class="mapping-hint">勾选要启用的上游模型；自定义公开名称在“配置映射”页签中逐行添加。</div>
          <div class="selection-toolbar"><el-input :model-value="discoveryKeyword" clearable placeholder="搜索上游模型" @update:model-value="emit('update:discoveryKeyword', $event)" /><el-button :disabled="!visibleDiscoveredModels.length" @click="toggleVisibleModels">{{ allVisibleSelected ? '取消当前结果' : '选择当前结果' }}</el-button></div>
          <div class="selection-summary"><span>已选择 {{ selectedModelNames.length }} 个</span><span>共发现 {{ discoveredModels.length }} 个</span></div>
          <el-checkbox-group v-if="visibleDiscoveredModels.length" :model-value="selectedModelNames" class="model-check-list" @update:model-value="emit('update:selectedModelNames', $event)"><div v-for="item in visibleDiscoveredModels" :key="item.name" class="model-selection-row"><el-checkbox :value="item.name"><code>{{ item.name }}</code></el-checkbox></div></el-checkbox-group>
          <div v-else-if="!discovering" class="selection-empty">{{ discoveredModels.length ? '没有匹配模型' : '上游没有返回模型' }}</div>
        </el-tab-pane>
        <el-tab-pane label="配置映射" name="mapping">
          <div class="mapping-toolbar"><span class="mapping-count">{{ modelMappings.length }} 条映射关系</span><el-button type="primary" :icon="Plus" @click="emit('addMapping')">添加映射</el-button></div>
          <div v-if="modelMappings.length" class="mapping-list">
            <div v-for="mapping in modelMappings" :key="mapping.id" class="mapping-entry-row">
              <el-select :model-value="mapping.upstreamName" filterable clearable placeholder="选择上游模型" class="mapping-upstream" @update:model-value="updateMapping(mapping.id, 'upstreamName', $event)"><el-option v-for="item in discoveredModels" :key="item.name" :label="item.name" :value="item.name" /></el-select>
              <el-input :model-value="mapping.publicName" maxlength="191" placeholder="填写自定义公开名称" class="mapping-public" @update:model-value="updateMapping(mapping.id, 'publicName', $event)" />
              <el-tooltip content="删除映射"><el-button text :icon="Trash2" :aria-label="`删除 ${mapping.upstreamName || '此条'} 映射`" @click="emit('removeMapping', mapping.id)" /></el-tooltip>
            </div>
          </div>
          <div v-else class="mapping-empty">暂无映射关系</div>
        </el-tab-pane>
      </el-tabs>
    </div>
    <template #footer><el-button @click="emit('update:modelValue', false)">取消</el-button><el-button type="primary" :loading="applying" :disabled="discovering || Boolean(discoveryError)" @click="emit('save')">保存映射</el-button></template>
  </el-dialog>
</template>

<style scoped>
.model-selection { min-height: 300px; }.mapping-hint { margin-bottom: 12px; color: #66717d; font-size: 12px; line-height: 1.5; }.selection-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }.selection-summary { display: flex; justify-content: space-between; margin: 13px 0 8px; color: #66717d; font-size: 11px; }.model-check-list { display: grid; max-height: 390px; overflow-y: auto; border-block: 1px solid #dce2e7; }.model-selection-row { display: flex; min-height: 52px; align-items: center; padding: 7px 10px; border-bottom: 1px solid #eef1f3; }.model-selection-row:last-child { border-bottom: 0; }.model-selection-row .el-checkbox { min-width: 0; margin: 0; }.model-selection-row .el-checkbox :deep(.el-checkbox__label) { overflow: hidden; text-overflow: ellipsis; }.model-selection-row code { overflow-wrap: anywhere; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.mapping-toolbar { display: flex; min-height: 36px; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }.mapping-count { color: #66717d; font-size: 12px; }.mapping-list { display: grid; max-height: 390px; overflow-y: auto; border-block: 1px solid #dce2e7; }.mapping-entry-row { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr) 38px; gap: 12px; align-items: center; min-height: 58px; padding: 8px 10px; border-bottom: 1px solid #eef1f3; }.mapping-entry-row:last-child { border-bottom: 0; }.mapping-upstream, .mapping-public { min-width: 0; }.mapping-empty, .selection-empty { display: grid; min-height: 220px; place-items: center; color: #66717d; font-size: 12px; }.discovery-error { display: flex; min-height: 220px; flex-direction: column; align-items: flex-start; justify-content: center; gap: 12px; padding: 20px; border: 1px solid #e9abb2; border-radius: 6px; color: #9c2836; background: #fff6f7; font-size: 12px; line-height: 1.55; }.discovery-error strong { font-size: 14px; }
@media (max-width: 600px) { .selection-toolbar { grid-template-columns: 1fr; }.mapping-entry-row { grid-template-columns: minmax(0, 1fr) 38px; }.mapping-upstream, .mapping-public { grid-column: 1; }.mapping-public { grid-row: 2; }.mapping-entry-row .el-tooltip { grid-column: 2; grid-row: 1 / span 2; } }
</style>
