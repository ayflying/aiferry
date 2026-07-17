<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { CircleAlert, CircleCheck, Gauge, Info, LoaderCircle, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost } from '../api/client'
import type { Channel, ChannelModel, ModelTestResult } from '../api/types'
import { showError } from '../lib/error'
import { formatLatency } from '../lib/format'
import { enabledChannelModels } from '../lib/models'

type TestEndpoint = 'auto' | ModelTestResult['endpoint']

const props = defineProps<{ modelValue: boolean; channel?: Channel }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; changed: [] }>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const loading = ref(false)
const running = ref(false)
const testingModelIDs = ref<number[]>([])
const batchTotal = ref(0)
const batchCompleted = ref(0)
const models = ref<ChannelModel[]>([])
const latestResults = ref<Record<number, ModelTestResult>>({})
const endpoint = ref<TestEndpoint>('auto')
const stream = ref(false)
const keyword = ref('')
const selectedModelID = ref<number>()
const page = ref(1)
const pageSize = ref(30)

const endpointOptions = [
  { label: '自动检测（默认）', value: 'auto' },
  { label: 'Chat Completions', value: 'chat' },
  { label: 'Responses', value: 'responses' },
  { label: 'Embeddings', value: 'embeddings' },
  { label: '图像生成', value: 'images' },
]
const enabledModels = computed(() => enabledChannelModels(models.value))
const filteredModels = computed(() => {
  const filter = keyword.value.trim().toLowerCase()
  if (!filter) return enabledModels.value
  return enabledModels.value.filter((model) => model.publicName.toLowerCase().includes(filter) || model.upstreamName.toLowerCase().includes(filter))
})
const pagedModels = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredModels.value.slice(start, start + pageSize.value)
})
const successCount = computed(() => enabledModels.value.filter((model) => statusOf(model) === 'success').length)
const failedModels = computed(() => enabledModels.value.filter((model) => statusOf(model) === 'failed'))
const hasActiveTests = computed(() => testingModelIDs.value.length > 0)
const batchButtonText = computed(() => running.value ? `测试中 ${batchCompleted.value}/${batchTotal.value}` : `测试全部 ${enabledModels.value.length} 个模型`)

watch(() => props.modelValue, (open) => {
  if (open) void loadModels()
})
watch(() => props.channel?.id, () => {
  if (visible.value) void loadModels()
})
watch([keyword, pageSize], () => { page.value = 1 })

async function loadModels(preserveResults = false) {
  if (!props.channel) return
  loading.value = true
  if (!preserveResults) latestResults.value = {}
  page.value = 1
  try {
    models.value = enabledChannelModels(await apiGet<ChannelModel[]>(`/channels/${props.channel.id}/models`))
    selectedModelID.value = models.value[0]?.id
  } catch (error) {
    showError(error, '加载测试模型失败')
  } finally {
    loading.value = false
  }
}

function statusOf(model: ChannelModel) {
  if (isTesting(model)) return 'testing'
  const result = latestResults.value[model.id]
  if (result) return result.success ? 'success' : 'failed'
  return model.lastTestStatus
}

function latencyOf(model: ChannelModel) {
  const result = latestResults.value[model.id]
  return result ? result.latencyMs : model.lastTestLatencyMs
}

function messageOf(model: ChannelModel) {
  if (isTesting(model)) return '测试中...'
  const result = latestResults.value[model.id]
  const message = result?.message || model.lastTestError || ''
  if (!result?.httpStatus) return message
  return `HTTP ${result.httpStatus} · ${message}`
}

function endpointOf(model: ChannelModel) {
  if (isTesting(model)) return endpoint.value === 'auto' ? '自动检测' : endpoint.value
  return latestResults.value[model.id]?.endpoint || model.lastTestEndpoint || ''
}

function isTesting(model: ChannelModel) {
  return testingModelIDs.value.includes(model.id)
}

function startTesting(model: ChannelModel) {
  testingModelIDs.value = [...testingModelIDs.value, model.id]
}

function finishTesting(model: ChannelModel) {
  testingModelIDs.value = testingModelIDs.value.filter((id) => id !== model.id)
}

function changePageSize(value: number | string) {
  pageSize.value = Number(value)
  page.value = 1
}

async function runTest(model: ChannelModel, quiet = false) {
  if (isTesting(model) || (!quiet && (running.value || hasActiveTests.value))) return
  startTesting(model)
  try {
    const result = await apiPost<ModelTestResult>('/models/test', {
      modelId: model.id,
      endpoint: endpoint.value,
      stream: stream.value,
    })
    latestResults.value = { ...latestResults.value, [model.id]: result }
    models.value = models.value.map((item) => item.id === model.id ? {
      ...item,
      lastTestEndpoint: result.endpoint,
      lastTestStatus: result.success ? 'success' : 'failed',
      lastTestLatencyMs: result.latencyMs,
      lastTestError: result.message,
    } : item)
    if (!quiet) {
      if (result.success) ElMessage.success(`${model.publicName} 测试通过`)
      else showError(testFailureMessage(result), `${model.publicName} 测试失败`)
      emit('changed')
    }
  } catch (error) {
    const failedEndpoint = endpoint.value === 'auto' ? 'chat' : endpoint.value
    latestResults.value = {
      ...latestResults.value,
      [model.id]: {
        success: false,
        endpoint: failedEndpoint,
        stream: false,
        model: model.publicName,
        latencyMs: 0,
        httpStatus: 0,
        inputTokens: 0,
        outputTokens: 0,
        message: error instanceof Error ? error.message : '测试请求失败',
      },
    }
    if (!quiet) showError(error, `${model.publicName} 测试失败`)
  } finally {
    finishTesting(model)
  }
}

async function testSelected() {
  const model = enabledModels.value.find((item) => item.id === selectedModelID.value)
  if (model) await runTest(model)
}

async function testAll() {
  if (!enabledModels.value.length) return
  const queue = [...enabledModels.value]
  running.value = true
  batchTotal.value = queue.length
  batchCompleted.value = 0
  try {
    let nextIndex = 0
    const runWorker = async () => {
      while (nextIndex < queue.length) {
        const model = queue[nextIndex]
        nextIndex += 1
        await runTest(model, true)
        batchCompleted.value += 1
      }
    }
    await Promise.all(Array.from({ length: Math.min(5, queue.length) }, runWorker))
    await loadModels(true)
    emit('changed')
    ElMessage.success(`已完成 ${queue.length} 个模型的测试`)
  } finally {
    running.value = false
    batchTotal.value = 0
    batchCompleted.value = 0
  }
}

function testFailureMessage(result: ModelTestResult) {
  const status = result.httpStatus ? `HTTP ${result.httpStatus}` : '未收到 HTTP 响应'
  return `${status} · ${result.endpoint} · ${result.message || '上游未通过测试'}`
}

async function deleteFailedModels() {
  if (!props.channel || !failedModels.value.length) return
  const count = failedModels.value.length
  try {
    await ElMessageBox.confirm(`将从渠道中移除 ${count} 个最近测试失败的模型。模型发现后仍可重新勾选恢复。`, '删除失败模型', {
      type: 'warning',
      confirmButtonText: '删除模型',
      cancelButtonText: '取消',
    })
    const result = await apiDelete<{ deleted: number }>(`/channels/${props.channel.id}/models/failed`)
    await loadModels()
    emit('changed')
    ElMessage.success(result.deleted ? `已删除 ${result.deleted} 个失败模型` : '没有可删除的失败模型')
  } catch (error) {
    if (error !== 'cancel') showError(error, '删除失败模型失败')
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="`测试渠道连接：${channel?.name || ''}`" class="channel-test-dialog" destroy-on-close>
    <div class="test-settings">
      <div class="setting-field">
        <label>端点类型</label>
        <el-select v-model="endpoint" :disabled="running || hasActiveTests">
          <el-option v-for="item in endpointOptions" :key="item.value" :label="item.label" :value="item.value" />
        </el-select>
        <small>选择模型测试端点；自动检测会按兼容端点依次尝试。</small>
      </div>
      <div class="setting-field">
        <label>流式模式</label>
        <el-switch v-model="stream" :disabled="running || hasActiveTests" active-text="已启用" inactive-text="已禁用" />
        <small>为支持流式响应的测试端点启用 stream。</small>
      </div>
    </div>

    <section class="model-test-section">
      <div class="test-section-heading">
        <div><h3>渠道模型</h3><p>选择模型后可单独测试，也可逐个测试全部已启用模型。</p></div>
        <el-input v-model="keyword" clearable placeholder="筛选模型..." :disabled="running || hasActiveTests" class="model-filter" />
      </div>
      <div class="test-actions">
        <el-button type="primary" :loading="running" :disabled="running || hasActiveTests || !enabledModels.length" @click="testAll">{{ batchButtonText }}</el-button>
        <span class="result-chip success"><CircleCheck :size="15" /> 测试通过（{{ successCount }}）</span>
        <el-button v-if="failedModels.length" plain type="danger" :icon="Trash2" :disabled="running || hasActiveTests" @click="deleteFailedModels">删除失败模型（{{ failedModels.length }}）</el-button>
      </div>

      <div v-loading="loading" class="test-table-wrap">
        <el-table v-if="pagedModels.length" :data="pagedModels" :show-header="false" size="small" class="test-model-table" height="360">
          <el-table-column width="44" align="center"><template #default="{ row }"><el-radio v-model="selectedModelID" :value="row.id" :disabled="hasActiveTests" :aria-label="`选择 ${row.publicName}`" /></template></el-table-column>
          <el-table-column min-width="260"><template #default="{ row }"><div class="model-name"><strong>{{ row.publicName }}</strong><small v-if="row.upstreamName !== row.publicName">{{ row.upstreamName }}</small></div></template></el-table-column>
          <el-table-column width="104"><template #default="{ row }"><span v-if="statusOf(row) === 'testing'" class="test-status testing"><LoaderCircle :size="15" class="spinner" />测试中...</span><span v-else-if="statusOf(row) === 'success'" class="test-status success">成功</span><span v-else-if="statusOf(row) === 'failed'" class="test-status failed">失败</span><span v-else class="test-status idle">未测试</span></template></el-table-column>
          <el-table-column min-width="235"><template #default="{ row }"><div v-if="statusOf(row) === 'testing'" class="test-outcome testing"><span>测试中...</span><small>{{ endpointOf(row) }}</small></div><div v-else-if="statusOf(row) === 'success'" class="test-outcome"><span>{{ formatLatency(latencyOf(row)) }}</span><small>{{ endpointOf(row) }}</small></div><div v-else-if="statusOf(row) === 'failed'" class="test-outcome failed"><span>{{ messageOf(row) }}</span><el-popover v-if="messageOf(row)" trigger="hover" placement="top" :width="360"><template #reference><button class="detail-button" type="button"><Info :size="15" />详情</button></template><p class="error-detail">{{ messageOf(row) }}</p></el-popover></div><span v-else class="muted">—</span></template></el-table-column>
          <el-table-column width="54" align="center"><template #default="{ row }"><el-tooltip :content="isTesting(row) ? '测试中' : '测试此模型'"><button class="icon-button" type="button" :aria-label="`${isTesting(row) ? '正在测试' : '测试'} ${row.publicName}`" :disabled="running || hasActiveTests" @click="runTest(row)"><LoaderCircle v-if="isTesting(row)" :size="17" class="spinner" /><Gauge v-else :size="17" /></button></el-tooltip></template></el-table-column>
        </el-table>
        <div v-else-if="!loading" class="test-empty"><CircleAlert :size="18" /><span>{{ enabledModels.length ? '没有匹配的模型' : '当前渠道没有已启用模型' }}</span></div>
      </div>
      <div class="test-pagination"><span>总计：{{ filteredModels.length }}</span><span>每页行数</span><el-select :model-value="pageSize" size="small" class="page-size" @update:model-value="changePageSize"><el-option :value="20" label="20" /><el-option :value="30" label="30" /><el-option :value="50" label="50" /></el-select><el-pagination background layout="prev, pager, next" :current-page="page" :page-size="pageSize" :total="filteredModels.length" @current-change="page = $event" /></div>
    </section>

    <template #footer><el-button :disabled="hasActiveTests" @click="visible = false">关闭</el-button><el-button type="primary" :disabled="!selectedModelID || running || hasActiveTests" @click="testSelected">{{ hasActiveTests ? '测试中...' : '测试选中模型' }}</el-button></template>
  </el-dialog>
</template>

<style scoped>
:deep(.channel-test-dialog) { width: min(880px, calc(100vw - 32px)) !important; }.test-settings { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; padding-bottom: 18px; border-bottom: 1px solid #dce2e7; }.setting-field { display: grid; gap: 8px; }.setting-field label, .test-section-heading h3 { color: #15202b; font-size: 13px; font-weight: 600; }.setting-field small, .test-section-heading p, .model-name small, .test-outcome small { color: #7b8792; font-size: 11px; }.model-test-section { padding-top: 18px; }.test-section-heading { display: flex; align-items: end; justify-content: space-between; gap: 18px; }.test-section-heading h3, .test-section-heading p { margin: 0; }.test-section-heading p { margin-top: 6px; }.model-filter { width: 258px; }.test-actions, .test-pagination { display: flex; align-items: center; gap: 9px; }.test-actions { min-height: 36px; margin: 12px 0; flex-wrap: wrap; }.result-chip, .test-status { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; font-weight: 600; }.result-chip { min-height: 30px; padding: 0 10px; border: 1px solid #dce2e7; border-radius: 15px; font-weight: 500; }.result-chip.success, .test-status.success { color: #16866f; }.test-status.failed { color: #c83e4d; }.test-status.testing, .test-outcome.testing { color: #1677ff; }.test-status.idle, .muted { color: #7b8792; }.test-table-wrap { overflow: hidden; border: 1px solid #dce2e7; border-radius: 8px; background: #fff; }.test-model-table :deep(.el-table__cell) { height: 52px; }.model-name, .test-outcome { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.model-name strong, .test-outcome > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.model-name strong { font-family: 'JetBrains Mono', monospace; font-size: 12px; }.test-outcome.failed { display: flex; flex-direction: row; align-items: center; color: #66717d; }.test-outcome.failed > span { flex: 1; }.detail-button { display: inline-flex; align-items: center; gap: 4px; border: 0; color: #40505f; background: transparent; cursor: pointer; font: inherit; font-size: 11px; }.detail-button:hover { color: #1677ff; }.spinner { animation: test-spin 0.9s linear infinite; }.error-detail { margin: 0; color: #40505f; overflow-wrap: anywhere; line-height: 1.55; }.test-empty { display: flex; min-height: 210px; align-items: center; justify-content: center; gap: 8px; color: #7b8792; font-size: 12px; }.test-pagination { justify-content: end; min-height: 48px; color: #7b8792; font-size: 11px; }.page-size { width: 68px; }@keyframes test-spin { to { transform: rotate(360deg); } }@media (max-width: 680px) { .test-settings { grid-template-columns: 1fr; gap: 14px; }.test-section-heading { align-items: stretch; flex-direction: column; gap: 12px; }.model-filter { width: 100%; }.test-table-wrap { overflow-x: auto; }.test-model-table { min-width: 700px; }.test-pagination { justify-content: flex-start; flex-wrap: wrap; } }@media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
</style>
