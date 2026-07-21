<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Copy, Eye, EyeOff, KeyRound, Pencil, Plus, RefreshCw, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { APIKey, CreatedAPIKey, PublicModel } from '../api/types'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { copyText } from '../lib/clipboard'
import { formatTime } from '../lib/format'
import TableActionButton from '../components/TableActionButton.vue'
import MobileRecordList from '../components/MobileRecordList.vue'
import ResponsiveList from '../components/ResponsiveList.vue'

const store = useAppStore()
const auth = useAuthStore()
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const dialogOpen = ref(false)
const editing = ref<APIKey>()
const created = ref<CreatedAPIKey>()
const models = ref<string[]>([])
const revealedSecrets = reactive<Record<number, string>>({})
const secretLoading = reactive<Record<number, boolean>>({})
const form = reactive<{ name: string; status: number; expiresAt?: Date; spendLimit?: number; dailySpendLimit?: number; allowedModels: string[]; channelGroupIds: number[] }>({ name: '', status: 1, expiresAt: undefined, spendLimit: undefined, dailySpendLimit: undefined, allowedModels: [], channelGroupIds: [] })
const isAdmin = computed(() => auth.user?.isAdmin === true)
const selectableModels = computed(() => [...new Set(models.value)].sort())

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const modelPromise = apiGet<PublicModel[]>('/public-models')
    const support = [store.loadAPIKeys()]
    if (isAdmin.value) support.push(store.loadChannelGroups())
    await Promise.all([modelPromise, ...support])
    models.value = (await modelPromise).map((item) => item.publicName)
  } catch (error) {
    loadError.value = (error as Error).message
    showError(loadError.value, '加载访问密钥失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = undefined
  created.value = undefined
  Object.assign(form, { name: '', status: 1, expiresAt: undefined, spendLimit: undefined, dailySpendLimit: undefined, allowedModels: [], channelGroupIds: [] })
  dialogOpen.value = true
}

function openEdit(item: APIKey) {
  editing.value = item
  created.value = undefined
  Object.assign(form, { name: item.name, status: item.status, expiresAt: item.expiresAt ? new Date(item.expiresAt) : undefined, spendLimit: item.spendLimit, dailySpendLimit: item.dailySpendLimit, allowedModels: [...(item.allowedModels ?? [])], channelGroupIds: [...(item.channelGroupIds ?? [])] })
  dialogOpen.value = true
}

async function save() {
  if (!form.name.trim()) { showError('请填写密钥名称', '信息不完整'); return }
  saving.value = true
  try {
    if (editing.value) {
      await apiPut(`/api-keys/${editing.value.id}`, { ...form, expiresAt: form.expiresAt?.toISOString() })
      ElMessage.success('访问密钥已更新')
      dialogOpen.value = false
    } else {
      created.value = await apiPost<CreatedAPIKey>('/api-keys', { ...form, expiresAt: form.expiresAt?.toISOString() })
      ElMessage.success('访问密钥已创建')
    }
    await load()
  } catch (error) { showError(error, '保存访问密钥失败') } finally { saving.value = false }
}

async function copyCreatedKey() {
  if (!created.value) return
  try {
    await copyText(created.value.key)
    ElMessage.success('密钥已复制')
  } catch (error) { showError(error, '复制密钥失败') }
}

function secretLabel(item: APIKey) {
  return revealedSecrets[item.id] || `${item.keyPrefix}••••`
}

function unavailableSecretLabel(item: APIKey) {
  return item.secretAvailable ? '' : '该密钥创建于加密保存启用前，无法恢复'
}

async function getSecret(item: APIKey) {
  return (await apiGet<{ key: string }>(`/api-keys/${item.id}/secret`)).key
}

async function copyListKey(item: APIKey) {
  if (!item.secretAvailable) return
  secretLoading[item.id] = true
  try {
    await copyText(await getSecret(item))
    ElMessage.success('完整密钥已复制')
  } catch (error) { showError(error, '复制完整密钥失败') } finally { secretLoading[item.id] = false }
}

async function toggleSecret(item: APIKey) {
  if (!item.secretAvailable) return
  if (revealedSecrets[item.id]) {
    delete revealedSecrets[item.id]
    return
  }
  secretLoading[item.id] = true
  try {
    revealedSecrets[item.id] = await getSecret(item)
  } catch (error) { showError(error, '显示完整密钥失败') } finally { secretLoading[item.id] = false }
}

async function remove(item: APIKey) {
  try {
    await ElMessageBox.confirm(`删除访问密钥“${item.name}”？`, '删除密钥', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/api-keys/${item.id}`)
    ElMessage.success('访问密钥已删除')
    await load()
  } catch (error) { if (error !== 'cancel') showError(error, '删除访问密钥失败') }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="muted">中转 API 使用独立密钥鉴权和归因</div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="openCreate">创建密钥</el-button>
    </div>

    <div class="table-panel">
      <div v-if="loadError && !store.apiKeys.length" class="empty-state error-state">
        <div><strong>访问密钥加载失败</strong><span>{{ loadError }}</span><el-button :icon="RefreshCw" @click="load">重新加载</el-button></div>
      </div>
      <ResponsiveList v-else-if="loading || store.apiKeys.length">
        <template #desktop><el-table v-loading="loading" :data="store.apiKeys" row-key="id">
        <el-table-column label="名称" min-width="170"><template #default="{ row }"><div class="key-name"><span class="key-icon"><KeyRound :size="15" /></span><strong>{{ row.name }}</strong></div></template></el-table-column>
        <el-table-column label="密钥" min-width="280"><template #default="{ row }"><div class="key-cell" :class="{ revealed: Boolean(revealedSecrets[row.id]) }"><span class="mono key-value">{{ secretLabel(row) }}</span><span class="table-actions"><TableActionButton :icon="Copy" :label="unavailableSecretLabel(row) || (secretLoading[row.id] ? '正在读取完整密钥' : '复制完整密钥')" :disabled="!row.secretAvailable || secretLoading[row.id]" @click="copyListKey(row)" /><TableActionButton :icon="revealedSecrets[row.id] ? Eye : EyeOff" :label="unavailableSecretLabel(row) || (secretLoading[row.id] ? '正在读取完整密钥' : (revealedSecrets[row.id] ? '隐藏完整密钥' : '显示完整密钥'))" :disabled="!row.secretAvailable || secretLoading[row.id]" @click="toggleSecret(row)" /></span></div></template></el-table-column>
        <el-table-column label="额度" min-width="180"><template #default="{ row }"><div class="amount-cell"><strong v-if="row.spendLimit !== undefined">总 {{ row.availableAmount?.toFixed(6) }} / {{ row.spentAmount.toFixed(6) }}</strong><strong v-else>总 不限额</strong><small v-if="row.dailySpendLimit !== undefined">每日 {{ row.dailyAvailableAmount?.toFixed(6) }} / {{ row.dailySpentAmount.toFixed(6) }}</small><small v-else>每日不限额</small></div></template></el-table-column>
        <el-table-column label="权限" min-width="150"><template #default="{ row }"><span class="muted">模型 {{ row.allowedModels?.length || '全部' }} · 分组 {{ row.channelGroupIds?.length || '全部' }}</span></template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column>
        <el-table-column label="过期时间" min-width="160"><template #default="{ row }">{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</template></el-table-column>
        <el-table-column label="最后使用" min-width="160"><template #default="{ row }">{{ formatTime(row.lastUsedAt) }}</template></el-table-column>
        <el-table-column label="创建时间" min-width="160"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><TableActionButton :icon="Pencil" label="编辑密钥" @click="openEdit(row)" /><TableActionButton :icon="Trash2" label="删除密钥" danger @click="remove(row)" /></div></template></el-table-column>
        </el-table></template>
        <template #mobile><MobileRecordList :loading="loading">
          <article v-for="row in store.apiKeys" :key="row.id" class="mobile-record">
            <div class="mobile-record__header"><div class="mobile-record__title"><strong>{{ row.name }}</strong><small>{{ row.status === 1 ? '可用于中转调用' : '此密钥已停用' }}</small></div><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></div>
            <div class="mobile-record__code"><code>{{ secretLabel(row) }}</code><div class="table-actions"><TableActionButton :icon="Copy" :label="unavailableSecretLabel(row) || (secretLoading[row.id] ? '正在读取完整密钥' : '复制完整密钥')" :disabled="!row.secretAvailable || secretLoading[row.id]" @click="copyListKey(row)" /><TableActionButton :icon="revealedSecrets[row.id] ? Eye : EyeOff" :label="unavailableSecretLabel(row) || (secretLoading[row.id] ? '正在读取完整密钥' : (revealedSecrets[row.id] ? '隐藏完整密钥' : '显示完整密钥'))" :disabled="!row.secretAvailable || secretLoading[row.id]" @click="toggleSecret(row)" /></div></div>
            <dl class="mobile-record__facts"><div><dt>总额度</dt><dd class="mono">{{ row.spendLimit !== undefined ? `${row.availableAmount?.toFixed(6)} / ${row.spentAmount.toFixed(6)}` : '不限额' }}</dd></div><div><dt>每日额度</dt><dd class="mono">{{ row.dailySpendLimit !== undefined ? `${row.dailyAvailableAmount?.toFixed(6)} / ${row.dailySpentAmount.toFixed(6)}` : '不限额' }}</dd></div><div><dt>过期时间</dt><dd>{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</dd></div><div><dt>最后使用</dt><dd>{{ formatTime(row.lastUsedAt) }}</dd></div><div class="mobile-record__wide"><dt>权限</dt><dd>模型 {{ row.allowedModels?.length || '全部' }} · 分组 {{ row.channelGroupIds?.length || '全部' }}</dd></div></dl>
            <div class="mobile-record__footer"><small class="muted">创建于 {{ formatTime(row.createdAt) }}</small><div class="mobile-record__actions"><el-button size="small" :icon="Pencil" @click="openEdit(row)">编辑</el-button><el-button size="small" :icon="Trash2" type="danger" plain @click="remove(row)">删除</el-button></div></div>
          </article>
        </MobileRecordList></template>
      </ResponsiveList>
      <div v-if="!loadError && !loading && !store.apiKeys.length" class="empty-state"><div><strong>还没有访问密钥</strong><span>创建密钥后才能调用 /v1 接口</span><el-button type="primary" :icon="Plus" @click="openCreate">创建密钥</el-button></div></div>
    </div>

    <el-dialog v-model="dialogOpen" :title="editing ? '编辑访问密钥' : '创建访问密钥'" width="min(520px, 92vw)" :close-on-click-modal="!created">
      <div v-if="created" class="secret-once">
        <strong>访问密钥已加密保存，可在列表中复制或显示</strong>
        <div class="secret-value"><code>{{ created.key }}</code><TableActionButton :icon="Copy" label="复制密钥" @click="copyCreatedKey" /></div>
      </div>
      <el-form v-else label-position="top">
        <el-form-item label="名称"><el-input v-model="form.name" placeholder="例如 开发环境" /></el-form-item>
        <el-form-item v-if="editing" label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" /></el-form-item>
        <div class="form-grid"><el-form-item label="总可用费用"><el-input-number v-model="form.spendLimit" :min="0" :precision="6" :controls="false" placeholder="不限额" style="width: 100%" /></el-form-item><el-form-item label="每日费用限额"><el-input-number v-model="form.dailySpendLimit" :min="0" :precision="6" :controls="false" placeholder="不限额" style="width: 100%" /></el-form-item></div>
        <el-form-item label="过期时间"><el-date-picker v-model="form.expiresAt" type="datetime" clearable placeholder="永不过期" style="width: 100%" /></el-form-item>
        <el-form-item label="可用大模型"><el-select v-model="form.allowedModels" multiple filterable clearable collapse-tags collapse-tags-tooltip placeholder="不选择表示可用全部已启用模型"><el-option v-for="model in selectableModels" :key="model" :label="model" :value="model" /></el-select></el-form-item>
        <el-form-item v-if="isAdmin" label="可用渠道分组"><el-select v-model="form.channelGroupIds" multiple filterable clearable collapse-tags collapse-tags-tooltip placeholder="不选择表示可用全部渠道分组"><el-option v-for="group in store.channelGroups.filter(item => item.status === 1)" :key="group.id" :label="`${group.name} (${group.code})`" :value="group.id" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button v-if="created" type="primary" @click="dialogOpen = false">完成</el-button><template v-else><el-button @click="dialogOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">{{ editing ? '保存密钥' : '创建密钥' }}</el-button></template></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.key-name { display: flex; align-items: center; gap: 9px; }.key-icon { display: grid; width: 28px; height: 28px; place-items: center; border-radius: 5px; color: #245f96; background: #e9f2ff; }.key-cell { display: flex; align-items: center; gap: 8px; min-width: 0; }.key-cell .key-value { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.key-cell.revealed { align-items: flex-start; }.key-cell.revealed .key-value { overflow: visible; overflow-wrap: anywhere; text-overflow: clip; white-space: normal; word-break: break-all; }.key-cell .table-actions { flex: 0 0 auto; }.amount-cell { display: flex; flex-direction: column; gap: 2px; font-family: 'JetBrains Mono', monospace; font-size: 11px; }.amount-cell small { color: #7b8792; }.empty-state span { display: block; margin-bottom: 14px; }.error-state strong { color: #c83e4d; }
</style>
