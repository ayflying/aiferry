<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { FlaskConical, Pencil, RefreshCw } from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { apiGet, apiPost, apiPut } from '../api/client'
import type { ChannelModel } from '../api/types'
import { useAppStore } from '../stores/app'
import { formatTime } from '../lib/format'

const store = useAppStore()
const models = ref<ChannelModel[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const channelId = ref<number>()
const enabledFilter = ref<string>('all')
const editOpen = ref(false)
const testOpen = ref(false)
const current = ref<ChannelModel>()
const form = reactive({ publicName: '', upstreamName: '', enabled: false, inputPrice: undefined as number | undefined, cachedInputPrice: undefined as number | undefined, outputPrice: undefined as number | undefined })
const testEndpoint = ref('chat')
const testResult = ref<{ success: boolean; latencyMs: number; httpStatus: number; inputTokens: number; outputTokens: number; message: string }>()

const filtered = computed(() => models.value.filter((item) => {
  if (channelId.value && item.channelId !== channelId.value) return false
  if (enabledFilter.value === 'enabled' && item.enabled !== 1) return false
  if (enabledFilter.value === 'disabled' && item.enabled === 1) return false
  const query = keyword.value.trim().toLowerCase()
  return !query || item.publicName.toLowerCase().includes(query) || item.upstreamName.toLowerCase().includes(query)
}))

async function load() {
  loading.value = true
  try {
    const [items] = await Promise.all([apiGet<ChannelModel[]>('/models'), store.loadChannels()])
    models.value = items
  } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}

function openEdit(model: ChannelModel) {
  current.value = model
  Object.assign(form, {
    publicName: model.publicName, upstreamName: model.upstreamName, enabled: model.enabled === 1,
    inputPrice: model.inputPrice, cachedInputPrice: model.cachedInputPrice, outputPrice: model.outputPrice,
  })
  editOpen.value = true
}

async function save() {
  if (!current.value || !form.publicName.trim() || !form.upstreamName.trim()) return
  saving.value = true
  try {
    await apiPut(`/models/${current.value.id}`, form)
    ElMessage.success('模型配置已保存')
    editOpen.value = false
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { saving.value = false }
}

function openTest(model: ChannelModel) {
  current.value = model
  testEndpoint.value = model.lastTestEndpoint || 'chat'
  testResult.value = undefined
  testOpen.value = true
}

async function testModel() {
  if (!current.value) return
  saving.value = true
  try {
    testResult.value = await apiPost('/models/test', { modelId: current.value.id, endpoint: testEndpoint.value })
    if (testResult.value?.success) ElMessage.success('模型测试通过')
    else ElMessage.error(testResult.value?.message || '模型测试失败')
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="toolbar-group">
        <el-input v-model="keyword" clearable placeholder="搜索模型" style="width: 220px" />
        <el-select v-model="channelId" clearable placeholder="全部渠道" style="width: 170px"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-select v-model="enabledFilter" style="width: 130px"><el-option label="全部状态" value="all" /><el-option label="已启用" value="enabled" /><el-option label="未启用" value="disabled" /></el-select>
      </div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="filtered" row-key="id">
        <el-table-column prop="publicName" label="公开模型" min-width="190"><template #default="{ row }"><span class="mono model-name">{{ row.publicName }}</span></template></el-table-column>
        <el-table-column prop="upstreamName" label="上游模型" min-width="190"><template #default="{ row }"><span class="mono muted">{{ row.upstreamName }}</span></template></el-table-column>
        <el-table-column prop="channelName" label="渠道" min-width="140" />
        <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.enabled === 1 ? 'success' : ''">{{ row.enabled === 1 ? '已启用' : '未启用' }}</span></template></el-table-column>
        <el-table-column label="价格 / 1M Token" min-width="230"><template #default="{ row }"><div class="price-line"><span>入 {{ row.inputPrice ?? '—' }}</span><span>缓存 {{ row.cachedInputPrice ?? '—' }}</span><span>出 {{ row.outputPrice ?? '—' }}</span></div></template></el-table-column>
        <el-table-column label="最近测试" min-width="150"><template #default="{ row }"><div v-if="row.lastTestStatus" class="test-cell"><span class="status-dot" :class="row.lastTestStatus">{{ row.lastTestStatus === 'success' ? `${row.lastTestLatencyMs} ms` : '失败' }}</span><small>{{ formatTime(row.lastTestAt) }}</small></div><span v-else class="muted">未测试</span></template></el-table-column>
        <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="测试模型"><button class="icon-button" @click="openTest(row)"><FlaskConical :size="16" /></button></el-tooltip><el-tooltip content="编辑"><button class="icon-button" @click="openEdit(row)"><Pencil :size="16" /></button></el-tooltip></div></template></el-table-column>
      </el-table>
      <div v-if="!loading && !filtered.length" class="empty-state"><div><strong>没有匹配模型</strong><span>先在渠道页发现模型，再在这里启用</span></div></div>
    </div>

    <el-drawer v-model="editOpen" title="模型配置" size="min(520px, 94vw)">
      <el-form label-position="top">
        <el-form-item label="公开模型名称"><el-input v-model="form.publicName" /></el-form-item>
        <el-form-item label="上游模型名称"><el-input v-model="form.upstreamName" /></el-form-item>
        <el-form-item label="对外启用"><el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" /></el-form-item>
        <div class="section-heading price-heading"><h2>USD / 1M Token</h2><span>留空表示未定价</span></div>
        <div class="form-grid">
          <el-form-item label="输入价格"><el-input-number v-model="form.inputPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item>
          <el-form-item label="缓存输入价格"><el-input-number v-model="form.cachedInputPrice" :min="0" :precision="6" :controls="false" placeholder="默认输入价" /></el-form-item>
          <el-form-item label="输出价格"><el-input-number v-model="form.outputPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item>
        </div>
      </el-form>
      <template #footer><el-button @click="editOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存模型</el-button></template>
    </el-drawer>

    <el-dialog v-model="testOpen" title="模型测试" width="min(520px, 92vw)">
      <div class="test-dialog">
        <div class="test-target"><span class="muted">模型</span><code>{{ current?.publicName }}</code></div>
        <el-segmented v-model="testEndpoint" :options="[{ label: 'Chat', value: 'chat' }, { label: 'Responses', value: 'responses' }, { label: 'Embeddings', value: 'embeddings' }]" />
        <div v-if="testResult" class="test-result" :class="testResult.success ? 'success' : 'failed'">
          <strong>{{ testResult.success ? '测试通过' : '测试失败' }}</strong>
          <span>HTTP {{ testResult.httpStatus || '—' }} · {{ testResult.latencyMs }} ms · 输入 {{ testResult.inputTokens }} · 输出 {{ testResult.outputTokens }}</span>
          <p>{{ testResult.message }}</p>
        </div>
      </div>
      <template #footer><el-button @click="testOpen = false">关闭</el-button><el-button type="primary" :loading="saving" @click="testModel">开始测试</el-button></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.model-name { color: #15202b; font-weight: 600; }.price-line { display: flex; gap: 12px; color: #4b5763; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.test-cell { display: flex; flex-direction: column; gap: 3px; }.test-cell small { color: #7b8792; font-size: 10px; }.price-heading { margin-top: 20px; padding-top: 16px; border-top: 1px solid #dce2e7; }.test-dialog { display: flex; flex-direction: column; gap: 16px; }.test-target { display: flex; align-items: center; justify-content: space-between; gap: 10px; }.test-target code { font-family: 'JetBrains Mono', monospace; font-size: 12px; }.test-result { padding: 13px; border: 1px solid #dce2e7; border-radius: 6px; }.test-result.success { border-color: #acd7cc; background: #f2faf8; }.test-result.failed { border-color: #e9abb2; background: #fff6f7; }.test-result span { display: block; margin-top: 5px; color: #66717d; font-size: 11px; }.test-result p { margin: 8px 0 0; font-size: 12px; }
</style>
