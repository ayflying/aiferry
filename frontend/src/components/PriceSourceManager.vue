<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { Pencil, Plus, RefreshCw, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiPost, apiPut } from '../api/client'
import type { PriceSource } from '../api/types'
import { showError } from '../lib/error'

const props = defineProps<{ modelValue: boolean; sources: PriceSource[]; loading: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; changed: [] }>()

const saving = ref(false)
const editing = ref<PriceSource>()
const form = reactive({ name: '', code: '', status: 1, configText: '' })
const drawerSize = window.innerWidth <= 600 ? '94%' : '680px'

watch(() => props.modelValue, (open) => {
  if (open) openCreate()
})

function close() {
  emit('update:modelValue', false)
}

function emptyConfig() {
  return JSON.stringify({ baseUrl: 'https://prices.example.com', pricing: { adapter: 'json', method: 'GET', path: '/v1/prices', authType: 'none', listPath: 'data', modelPath: 'model', ratesPath: 'rates' } }, null, 2)
}

function openCreate() {
  editing.value = undefined
  Object.assign(form, { name: '', code: '', status: 1, configText: emptyConfig() })
}

function openEdit(source: PriceSource) {
  editing.value = source
  Object.assign(form, { name: source.name, code: source.code, status: source.status, configText: JSON.stringify(source.config, null, 2) })
}

async function save() {
  let config: Record<string, unknown>
  try { config = JSON.parse(form.configText) } catch { showError('价格源 JSON 格式无效', '格式错误'); return }
  saving.value = true
  try {
    const payload = { name: form.name.trim(), code: form.code.trim(), status: form.status, config }
    if (editing.value) await apiPut(`/price-sources/${editing.value.id}`, payload)
    else await apiPost('/price-sources', payload)
    ElMessage.success(editing.value ? '价格源已更新' : '价格源已添加')
    editing.value = undefined
    emit('changed')
  } catch (error) { showError(error, '保存价格源失败') } finally { saving.value = false }
}

async function remove(source: PriceSource) {
  try {
    await ElMessageBox.confirm(`删除价格源“${source.name}”？`, '删除价格源', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/price-sources/${source.id}`)
    ElMessage.success('价格源已删除')
    emit('changed')
  } catch (error) { if (error !== 'cancel') showError(error, '删除价格源失败') }
}
</script>

<template>
  <el-drawer :model-value="props.modelValue" title="价格同步来源" :size="drawerSize" @update:model-value="emit('update:modelValue', $event)">
    <div class="page-toolbar"><div class="muted">公共价格源不属于任何渠道。</div><div class="spacer" /><el-button :icon="RefreshCw" :loading="props.loading" @click="emit('changed')">刷新</el-button><el-button type="primary" :icon="Plus" @click="openCreate">添加价格源</el-button></div>
    <div class="table-panel"><el-table v-loading="props.loading" :data="props.sources" row-key="id"><el-table-column label="名称" min-width="180"><template #default="{ row }"><div class="source-cell"><strong>{{ row.name }}</strong><code>{{ row.code }}</code></div></template></el-table-column><el-table-column label="地址" min-width="200"><template #default="{ row }"><span class="mono">{{ row.config.baseUrl }}{{ row.config.pricing.path }}</span></template></el-table-column><el-table-column label="状态" width="82"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column><el-table-column label="操作" width="96" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="编辑价格源"><button class="icon-button" type="button" @click="openEdit(row)"><Pencil :size="16" /></button></el-tooltip><el-tooltip :disabled="row.builtIn === 0" :content="row.builtIn === 1 ? '内置价格源不可删除' : ''"><button class="icon-button danger" type="button" :disabled="row.builtIn === 1" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column></el-table><div v-if="!props.loading && !props.sources.length" class="empty-state"><div><strong>还没有价格同步来源</strong><span>添加公开价格接口后可同步公共模型价格</span></div></div></div>

    <el-divider />
    <el-form label-position="top"><div class="form-grid"><el-form-item label="名称"><el-input v-model="form.name" /></el-form-item><el-form-item label="代码"><el-input v-model="form.code" :disabled="Boolean(editing)" placeholder="例如 vendor_prices" /></el-form-item><el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item></div><el-form-item label="价格源 JSON 配置"><el-input v-model="form.configText" class="config-editor" type="textarea" :rows="16" spellcheck="false" /></el-form-item><el-button type="primary" :loading="saving" @click="save">{{ editing ? '保存价格源' : '添加价格源' }}</el-button></el-form>
    <template #footer><el-button @click="close">关闭</el-button></template>
  </el-drawer>
</template>

<style scoped>
.page-toolbar { display: flex; min-height: 36px; align-items: center; gap: 10px; margin-bottom: 16px; }.page-toolbar .spacer { flex: 1; }.source-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.source-cell strong { font-size: 13px; }.source-cell code { color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.config-editor :deep(textarea) { min-height: 320px !important; font-family: 'JetBrains Mono', monospace; font-size: 11px; line-height: 1.55; }
</style>
