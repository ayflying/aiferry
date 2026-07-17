<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Braces, Coins, RefreshCw, RotateCw, Trash2 } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { ModelBillingMode, PriceRule, PriceSource, PublicModel } from '../api/types'
import { showError } from '../lib/error'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { compareModelNames } from '../lib/models'
import TableActionButton from '../components/TableActionButton.vue'
import PriceSourceManager from '../components/PriceSourceManager.vue'

const store = useAppStore()
const auth = useAuthStore()
const models = ref<PublicModel[]>([])
const sources = ref<PriceSource[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const priceSyncTarget = ref('')
const editOpen = ref(false)
const sourceOpen = ref(false)
const current = ref<PublicModel>()
const rules = ref<PriceRule[]>([])
const ruleSaving = ref(false)
const ruleForm = reactive({ name: '', priority: 100, currency: 'USD', status: 1, conditionsText: '{\n  "endpoint": ""\n}', ratesText: '{\n  "inputPerMillion": 0,\n  "cachedInputPerMillion": 0,\n  "cacheWritePerMillion": 0,\n  "outputPerMillion": 0,\n  "imageInputPerMillion": 0,\n  "audioInputPerMillion": 0,\n  "audioOutputPerMillion": 0,\n  "request": 0\n}' })
const priceDrawerSize = window.innerWidth <= 600 ? '94%' : '520px'
const form = reactive({
  billingMode: 'token' as ModelBillingMode,
  inputPrice: undefined as number | undefined,
  cachedInputPrice: undefined as number | undefined,
  cacheWritePrice: undefined as number | undefined,
  outputPrice: undefined as number | undefined,
  imageInputPrice: undefined as number | undefined,
  audioInputPrice: undefined as number | undefined,
  audioOutputPrice: undefined as number | undefined,
  requestPrice: undefined as number | undefined,
})
const isAdmin = computed(() => auth.user?.isAdmin === true)

const filtered = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return models.value.filter((item) => !query || item.publicName.toLowerCase().includes(query)).sort((left, right) => compareModelNames(left.publicName, right.publicName))
})
const enabledSources = computed(() => sources.value.filter((item) => item.status === 1))
const channelPriceSources = computed(() => {
  const typesWithPricing = new Set(store.channelTypes.filter((type) => type.config.pricing.adapter !== 'none').map((type) => type.code))
  return store.channels.filter((channel) => channel.status === 1 && typesWithPricing.has(channel.type))
})

async function load() {
  loading.value = true
  try {
    if (isAdmin.value) {
      const [items, priceSources] = await Promise.all([
        apiGet<PublicModel[]>('/public-models'),
        apiGet<PriceSource[]>('/price-sources'),
        store.loadChannels(),
        store.loadChannelTypes(),
      ])
      models.value = items
      sources.value = priceSources
    } else {
      models.value = await apiGet<PublicModel[]>('/public-models')
      sources.value = []
    }
    if (!syncTargetExists(priceSyncTarget.value)) priceSyncTarget.value = defaultSyncTarget()
  } catch (error) { showError(error, '加载模型失败') } finally { loading.value = false }
}

function defaultSyncTarget() {
  return enabledSources.value[0] ? `source:${enabledSources.value[0].id}` : channelPriceSources.value[0] ? `channel:${channelPriceSources.value[0].id}` : ''
}

function syncTargetExists(target: string) {
  const [kind, rawID] = target.split(':')
  const id = Number(rawID)
  return kind === 'source' ? enabledSources.value.some((item) => item.id === id) : kind === 'channel' && channelPriceSources.value.some((item) => item.id === id)
}

function openEdit(model: PublicModel) {
  current.value = model
  Object.assign(form, {
    billingMode: model.billingMode || 'token',
    inputPrice: model.inputPrice,
    cachedInputPrice: model.cachedInputPrice,
    cacheWritePrice: model.cacheWritePrice,
    outputPrice: model.outputPrice,
    imageInputPrice: model.imageInputPrice,
    audioInputPrice: model.audioInputPrice,
    audioOutputPrice: model.audioOutputPrice,
    requestPrice: model.requestPrice,
  })
  editOpen.value = true
  loadRules(model.id)
}

async function loadRules(modelId: number) {
  try { rules.value = await apiGet<PriceRule[]>(`/models/${modelId}/price-rules`) } catch (error) { showError(error, '加载价格规则失败') }
}

async function save() {
  if (!current.value) return
  saving.value = true
  try {
    await apiPut(`/models/${current.value.id}`, { ...form })
    ElMessage.success('公共模型价格已保存')
    editOpen.value = false
    await load()
  } catch (error) { showError(error, '保存公共价格失败') } finally { saving.value = false }
}

async function addRule() {
  if (!current.value) return
  let conditions: Record<string, unknown>; let rates: Record<string, number>
  try { conditions = JSON.parse(ruleForm.conditionsText); rates = JSON.parse(ruleForm.ratesText) } catch { showError('规则条件或费率 JSON 格式无效', '格式错误'); return }
  ruleSaving.value = true
  try {
    await apiPost(`/models/${current.value.id}/price-rules`, { name: ruleForm.name.trim() || '人工规则', source: 'manual', sourceRef: '', priority: ruleForm.priority, currency: ruleForm.currency, conditions, rates, status: ruleForm.status })
    form.billingMode = 'rules'
    await apiPut(`/models/${current.value.id}`, { billingMode: form.billingMode })
    ElMessage.success('高级价格规则已添加')
    await loadRules(current.value.id)
  } catch (error) { showError(error, '添加价格规则失败') } finally { ruleSaving.value = false }
}

async function removeRule(rule: PriceRule) {
  try {
    await ElMessageBox.confirm(`删除价格规则“${rule.name}”？`, '删除价格规则', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
    await apiDelete(`/price-rules/${rule.id}`)
    if (current.value) await loadRules(current.value.id)
  } catch (error) { if (error !== 'cancel') showError(error, '删除价格规则失败') }
}

async function syncPrices() {
  if (!priceSyncTarget.value) { showError('请选择价格同步来源', '无法同步价格'); return }
  const [kind, rawID] = priceSyncTarget.value.split(':')
  loading.value = true
  try {
    const payload = kind === 'source' ? { priceSourceId: Number(rawID) } : { channelId: Number(rawID) }
    const result = await apiPost<{ count: number; sources: number; succeeded: number; failures: Array<{ sourceName?: string; channelName?: string; message: string }> }>('/prices/sync', payload)
    if (!result.succeeded) showError(formatSyncFailures(result.failures), '价格同步失败')
    else if (result.failures.length) showError(`已同步 ${result.count} 条公共价格规则，但以下来源未完成：${formatSyncFailures(result.failures)}`, '价格同步未完全完成')
    else ElMessage.success(result.count ? `已同步 ${result.count} 条公共价格规则` : '所选来源没有返回已匹配的公开模型价格')
    if (current.value) await loadRules(current.value.id)
    await load()
  } catch (error) { showError(error, '价格同步失败') } finally { loading.value = false }
}

function formatSyncFailures(failures: Array<{ sourceName?: string; channelName?: string; message: string }>) {
  const visible = failures.slice(0, 2).map((item) => `${item.sourceName || item.channelName || '价格源'}：${item.message}`)
  const remaining = failures.length - visible.length
  return `${visible.join('；')}${remaining > 0 ? `；另有 ${remaining} 个价格源失败` : ''}`
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="toolbar-group"><el-input v-model="keyword" clearable placeholder="搜索公开模型" style="width: 240px" /></div>
      <div class="spacer" />
      <template v-if="isAdmin">
      <el-select v-model="priceSyncTarget" clearable filterable no-data-text="没有可用价格源" placeholder="选择价格同步来源" style="width: 230px">
        <el-option-group v-if="enabledSources.length" label="公共价格源"><el-option v-for="item in enabledSources" :key="`source:${item.id}`" :label="item.name" :value="`source:${item.id}`" /></el-option-group>
        <el-option-group v-if="channelPriceSources.length" label="渠道"><el-option v-for="item in channelPriceSources" :key="`channel:${item.id}`" :label="item.name" :value="`channel:${item.id}`" /></el-option-group>
      </el-select>
      <el-button :icon="Braces" @click="sourceOpen = true">价格源</el-button>
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
      <el-button :icon="RotateCw" :loading="loading" :disabled="!priceSyncTarget" @click="syncPrices">同步模型价格</el-button>
      </template>
      <el-button v-else :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="filtered" row-key="publicName">
        <el-table-column prop="publicName" label="公开模型" min-width="250"><template #default="{ row }"><span class="mono model-name">{{ row.publicName }}</span></template></el-table-column>
        <el-table-column label="计费方式" min-width="260"><template #default="{ row }"><div class="price-line"><template v-if="row.billingMode === 'request'"><span>按次 {{ row.requestPrice ?? '—' }}</span></template><template v-else-if="row.billingMode === 'rules'"><span>高级计费规则</span></template><template v-else><span>入 {{ row.inputPrice ?? '—' }}</span><span>缓存读 {{ row.cachedInputPrice ?? '—' }}</span><span>补全 {{ row.outputPrice ?? '—' }}</span></template></div></template></el-table-column>
        <el-table-column v-if="isAdmin" label="操作" width="86" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><TableActionButton :icon="Coins" :label="`设置 ${row.publicName} 的公共价格`" @click="openEdit(row)" /></div></template></el-table-column>
      </el-table>
      <div v-if="!loading && !filtered.length" class="empty-state"><div><strong>没有匹配模型</strong><span>先在渠道页发现并选择模型</span></div></div>
    </div>

    <el-drawer v-if="isAdmin" v-model="editOpen" title="公共价格设置" :size="priceDrawerSize">
      <el-form label-position="top">
        <div class="price-target"><div><span>公开模型</span><code>{{ current?.publicName }}</code></div><div><span>适用范围</span><strong>所有同名模型</strong></div></div>
        <el-tabs v-model="form.billingMode" class="pricing-tabs">
          <el-tab-pane label="按 Token" name="token">
            <div class="section-heading price-heading"><h2>USD / 1M Token</h2><span>留空表示该维度不计费</span></div>
            <div class="form-grid"><el-form-item label="输入价格"><el-input-number v-model="form.inputPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item><el-form-item label="补全价格"><el-input-number v-model="form.outputPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item><el-form-item label="缓存读取价格"><el-input-number v-model="form.cachedInputPrice" :min="0" :precision="6" :controls="false" placeholder="默认输入价" /></el-form-item><el-form-item label="缓存写入价格"><el-input-number v-model="form.cacheWritePrice" :min="0" :precision="6" :controls="false" placeholder="默认输入价" /></el-form-item><el-form-item label="图像输入价格"><el-input-number v-model="form.imageInputPrice" :min="0" :precision="6" :controls="false" placeholder="默认输入价" /></el-form-item><el-form-item label="音频输入价格"><el-input-number v-model="form.audioInputPrice" :min="0" :precision="6" :controls="false" placeholder="默认输入价" /></el-form-item><el-form-item label="音频输出价格"><el-input-number v-model="form.audioOutputPrice" :min="0" :precision="6" :controls="false" placeholder="默认补全价" /></el-form-item></div>
          </el-tab-pane>
          <el-tab-pane label="按次" name="request">
            <div class="section-heading price-heading"><h2>固定价格</h2><span>每请求的美元费用，不考虑使用的令牌数</span></div>
            <el-form-item label="固定价格（USD / 请求）"><el-input-number v-model="form.requestPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item>
          </el-tab-pane>
          <el-tab-pane label="高级计费规则" name="rules">
            <div class="section-heading price-heading"><h2>高级计费规则</h2><span>仅在此页签启用时参与计费</span></div>
            <div class="rules-list"><div v-for="rule in rules" :key="rule.id" class="rule-row"><div><strong>{{ rule.name }}</strong><span>{{ rule.source === 'sync' ? '上游同步' : '人工规则' }} · P{{ rule.priority }} · {{ rule.currency }}</span></div><code>{{ JSON.stringify(rule.rates) }}</code><TableActionButton :icon="Trash2" label="删除规则" danger :size="15" @click="removeRule(rule)" /></div><div v-if="!rules.length" class="muted">没有高级规则。</div></div>
            <div class="rule-editor"><el-input v-model="ruleForm.name" placeholder="规则名称，例如 Chat 长上下文" /><div class="form-grid"><el-input-number v-model="ruleForm.priority" :min="-999" :max="999" controls-position="right" /><el-input v-model="ruleForm.currency" maxlength="12" /></div><el-input v-model="ruleForm.conditionsText" type="textarea" :rows="4" spellcheck="false" /><el-input v-model="ruleForm.ratesText" type="textarea" :rows="8" spellcheck="false" /><el-button :loading="ruleSaving" @click="addRule">添加人工规则</el-button></div>
          </el-tab-pane>
        </el-tabs>
      </el-form>
      <template #footer><el-button @click="editOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存价格</el-button></template>
    </el-drawer>
    <PriceSourceManager v-if="isAdmin" v-model="sourceOpen" :sources="sources" :loading="loading" @changed="load" />
  </div>
</template>

<style scoped>
.model-name { color: #15202b; font-weight: 600; }.price-line { display: flex; gap: 12px; color: #4b5763; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.price-target { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; padding: 13px; border: 1px solid #dce2e7; border-radius: 6px; background: #f7f9fa; }.price-target div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.price-target span { color: #66717d; font-size: 11px; }.price-target code, .price-target strong { overflow: hidden; font-family: 'JetBrains Mono', monospace; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }.pricing-tabs { margin-top: 18px; }.pricing-tabs :deep(.el-tabs__header) { margin-bottom: 14px; }.pricing-tabs :deep(.el-input-number) { width: 100%; }.price-heading { margin-top: 4px; padding-top: 0; border-top: 0; }.rules-list { display: grid; gap: 7px; margin: 10px 0; }.rule-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr) auto; gap: 8px; align-items: center; padding: 8px; border: 1px solid #dce2e7; border-radius: 6px; }.rule-row div { display: flex; flex-direction: column; gap: 2px; }.rule-row span { color: #66717d; font-size: 10px; }.rule-row code { overflow: hidden; color: #4b5763; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.rule-editor { display: grid; gap: 9px; margin-top: 12px; padding-top: 12px; border-top: 1px solid #dce2e7; }.rule-editor :deep(textarea) { font-family: 'JetBrains Mono', monospace; font-size: 11px; }
</style>
