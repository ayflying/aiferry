<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Braces, Plus, Trash2 } from '@lucide/vue'
import { ElMessage } from 'element-plus'

import {
  AUDIO_ADAPTER_OPTIONS, AUTH_TYPE_OPTIONS, COST_ADAPTER_OPTIONS, METHOD_OPTIONS,
  PRICING_ADAPTER_OPTIONS, PRICING_PATH_FIELDS, QUOTA_ADAPTER_OPTIONS, REQUEST_BODY_OPTIONS,
  VALUE_TYPE_OPTIONS, VIDEO_ADAPTER_OPTIONS,
  audioVideoSummary, configForForm, costsShowsBalanceFields, costsShowsUsageFields, costsSummary,
  createEndpointConfig, endpointNames, endpointSummary, formatTypeConfig, modelsSummary,
  parseTypeConfigText, pricingDetailVisible, pricingSummary, protocolSummary,
  quotaDetailLocked, quotaDetailVisible, quotaSummary, renameEndpoint,
} from '../lib/channelTypeConfig'
import type { TypeConfigRecord } from '../lib/channelTypeConfig'
import TableActionButton from './TableActionButton.vue'

const props = withDefaults(defineProps<{ readonly?: boolean }>(), { readonly: false })
const text = defineModel<string>('configText', { required: true })

const mode = ref<'form' | 'json'>('form')
const form = ref<TypeConfigRecord>(configForForm(null))
const active = ref<string[]>(['base', 'models'])
const editingNames = ref<Record<string, string>>({})
// echo 记住最近一次由表单写出去的 JSON：外部赋值（打开新增/编辑）必须重建表单，
// 但我们自己写回的文本不能再反过来覆盖表单，否则用户正在输入的内容会被重置。
// 回声只吞一次——父级可能「先清空再回填」，若把内容相同当永久回声，回填会被误吞、
// 界面停在空骨架（0.5.114 在关闭时间面板上踩过同一个坑）。
let echo = ''

watch(text, (value) => {
  if (mode.value === 'json') return
  if (echo && value === echo) { echo = ''; return }
  form.value = configForForm(parseTypeConfigText(value).config)
}, { immediate: true })

// 是否需要写回，用「表单的规范化形状」与「文本的规范化形状」比较得出，
// 不能用「跳过下一次」这类标记：同一轮 flush 内 Vue 会合并多次触发，
// 标记会被外部同步消费掉，真正的用户编辑反而被当成同步忽略。
watch(form, () => {
  if (mode.value !== 'form') return
  const next = formatTypeConfig(form.value)
  const current = formatTypeConfig(configForForm(parseTypeConfigText(text.value).config))
  if (next === current) return
  echo = next
  text.value = next
}, { deep: true })

const jsonError = computed(() => (mode.value === 'json' ? parseTypeConfigText(text.value).error : ''))
const showBalanceFields = computed(() => costsShowsBalanceFields(form.value))
const showUsageFields = computed(() => costsShowsUsageFields(form.value))
const showPricingDetail = computed(() => pricingDetailVisible(form.value))
const showQuotaDetail = computed(() => quotaDetailVisible(form.value))
const quotaLocked = computed(() => quotaDetailLocked(form.value))
const endpoints = computed(() => endpointNames(form.value))

function setMode(next: 'form' | 'json') {
  if (next === mode.value) return
  if (next === 'form') form.value = configForForm(parseTypeConfigText(text.value).config)
  mode.value = next
}

function formatJson() {
  const { config, error } = parseTypeConfigText(text.value)
  if (error || !config) { ElMessage.warning('JSON 格式有误，无法格式化'); return }
  text.value = formatTypeConfig(config)
}

function endpointName(name: string) {
  return editingNames.value[name] ?? name
}

function commitEndpointName(name: string) {
  const next = (editingNames.value[name] ?? name).trim()
  delete editingNames.value[name]
  if (next === name) return
  if (!renameEndpoint(form.value, name, next)) {
    ElMessage.warning(`端点名「${next}」不可用（为空或已存在）`)
  }
}

function addEndpoint() {
  const endpoints = form.value.endpoints
  const target = endpoints && typeof endpoints === 'object' && !Array.isArray(endpoints) ? endpoints : {}
  let index = 1
  let name = `customEndpoint${index}`
  while (Object.prototype.hasOwnProperty.call(target, name)) { index += 1; name = `customEndpoint${index}` }
  target[name] = createEndpointConfig()
  form.value.endpoints = target
}

function removeEndpoint(name: string) {
  const endpoints = form.value.endpoints
  if (!endpoints || typeof endpoints !== 'object') return
  if (endpointNames(form.value).length <= 1) {
    ElMessage.warning('至少保留一个端点；若要全部使用内置端点，请点「改用内置端点」')
    return
  }
  delete endpoints[name]
}

// 完全不声明 endpoints 时后端会回落到内置端点表，这是合法且常用的写法。
function useBuiltinEndpoints() {
  delete form.value.endpoints
  ElMessage.success('已移除自定义端点，保存后使用内置端点表')
}
</script>

<template>
  <div class="type-config-editor">
    <div class="editor-head">
      <div class="editor-head__text">
        <strong>类型配置</strong>
        <span>按分组填写常用字段；需要精细控制时切到 JSON，两种视图共用同一份配置。</span>
      </div>
      <el-radio-group :model-value="mode" size="small" @update:model-value="setMode($event as 'form' | 'json')">
        <el-radio-button value="form">表单</el-radio-button>
        <el-radio-button value="json">JSON</el-radio-button>
      </el-radio-group>
    </div>

    <el-form v-if="mode === 'form'" label-position="top" class="type-config-form" :disabled="props.readonly">
      <el-collapse v-model="active">
        <el-collapse-item name="base">
          <template #title><span class="collapse-title">基础与协议<i>{{ protocolSummary(form) }}</i></span></template>
          <el-form-item label="API 根地址"><el-input v-model="form.baseUrl" placeholder="https://api.example.com/v1" spellcheck="false" /></el-form-item>
          <div class="setting-row">
            <div><strong>仅提供 Chat Completions</strong><span>上游没有 /responses 端点时开启，避免每次请求先转投再回退</span></div>
            <el-switch v-model="form.protocol.chatCompletionsOnly" />
          </div>
        </el-collapse-item>

        <el-collapse-item name="models">
          <template #title><span class="collapse-title">模型发现<i>{{ modelsSummary(form) }}</i></span></template>
          <p class="section-hint">请求方法固定为 GET；路径相对上方 API 根地址解析。</p>
          <div class="field-grid">
            <el-form-item class="span-all" label="模型列表路径"><el-input v-model="form.models.path" placeholder="/models" spellcheck="false" /></el-form-item>
            <el-form-item label="列表所在字段"><el-input v-model="form.models.listPath" placeholder="data（响应本身是数组时留空）" spellcheck="false" /></el-form-item>
            <el-form-item label="模型 ID 字段"><el-input v-model="form.models.idPath" placeholder="id" spellcheck="false" /></el-form-item>
            <el-form-item label="鉴权方式"><el-select v-model="form.models.authType"><el-option v-for="item in AUTH_TYPE_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
            <el-form-item label="请求头名称"><el-input v-model="form.models.headerName" placeholder="Authorization" spellcheck="false" /></el-form-item>
            <el-form-item class="span-all" label="请求头前缀"><el-input v-model="form.models.headerPrefix" placeholder="Bearer（自定义头名且无前缀时填「空」）" spellcheck="false" /><span class="field-hint">自定义头名（如 api-key）需同时把前缀置空，否则会带上默认的 Bearer。</span></el-form-item>
          </div>
        </el-collapse-item>

        <el-collapse-item name="costs">
          <template #title><span class="collapse-title">费用与额度查询<i>{{ costsSummary(form) }}</i></span></template>
          <div class="field-grid">
            <el-form-item label="适配器"><el-select v-model="form.costs.adapter"><el-option v-for="item in COST_ADAPTER_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
            <el-form-item label="查询语义"><el-select v-model="form.costs.valueType"><el-option v-for="item in VALUE_TYPE_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
          </div>
          <template v-if="form.costs.adapter !== 'none'">
            <p class="section-hint">请求方法固定为 GET；路径相对上方 API 根地址解析。</p>
            <div class="field-grid">
              <el-form-item class="span-all" label="查询路径"><el-input v-model="form.costs.path" placeholder="/organization/costs" spellcheck="false" /></el-form-item>
              <el-form-item label="鉴权方式"><el-select v-model="form.costs.authType"><el-option v-for="item in AUTH_TYPE_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
              <el-form-item label="请求头名称"><el-input v-model="form.costs.headerName" placeholder="Authorization" spellcheck="false" /></el-form-item>
              <el-form-item class="span-all" label="请求头前缀"><el-input v-model="form.costs.headerPrefix" placeholder="Bearer " spellcheck="false" /></el-form-item>
              <template v-if="showUsageFields">
                <el-form-item class="span-all" label="用量数值字段"><el-input v-model="form.costs.usagePath" placeholder="data.usage" spellcheck="false" /></el-form-item>
                <el-form-item label="用量单位"><el-input v-model="form.costs.usageUnit" placeholder="tokens / calls" spellcheck="false" /></el-form-item>
                <el-form-item label="用量类型"><el-input v-model="form.costs.usageType" spellcheck="false" /></el-form-item>
                <el-form-item class="span-all" label="用量维度"><el-input v-model="form.costs.usageDimension" spellcheck="false" /></el-form-item>
              </template>
              <template v-else-if="showBalanceFields">
                <el-form-item label="已用数值字段"><el-input v-model="form.costs.usedPath" placeholder="data.used" spellcheck="false" /></el-form-item>
                <el-form-item label="剩余数值字段"><el-input v-model="form.costs.remainingPath" placeholder="data.remaining" spellcheck="false" /></el-form-item>
                <el-form-item label="货币字段"><el-input v-model="form.costs.currencyPath" placeholder="data.currency" spellcheck="false" /></el-form-item>
                <el-form-item label="固定货币"><el-input v-model="form.costs.fixedCurrency" placeholder="USD / CNY" spellcheck="false" /></el-form-item>
              </template>
            </div>
          </template>
        </el-collapse-item>

        <el-collapse-item name="pricing">
          <template #title><span class="collapse-title">价格同步<i>{{ pricingSummary(form) }}</i></span></template>
          <div class="field-grid">
            <el-form-item label="适配器"><el-select v-model="form.pricing.adapter"><el-option v-for="item in PRICING_ADAPTER_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
          </div>
          <template v-if="showPricingDetail">
            <p class="section-hint">从上游接口读取价格表并写入公共价格。请求方法固定为 GET。</p>
            <div class="field-grid">
              <el-form-item class="span-all" label="价格接口路径"><el-input v-model="form.pricing.path" placeholder="/api/pricing" spellcheck="false" /></el-form-item>
              <el-form-item label="鉴权方式"><el-select v-model="form.pricing.authType"><el-option v-for="item in AUTH_TYPE_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
              <el-form-item label="请求头名称"><el-input v-model="form.pricing.headerName" placeholder="Authorization" spellcheck="false" /></el-form-item>
              <el-form-item class="span-all" label="请求头前缀"><el-input v-model="form.pricing.headerPrefix" placeholder="Bearer " spellcheck="false" /></el-form-item>
              <el-form-item v-for="field in PRICING_PATH_FIELDS" :key="field.key" :label="field.label" :class="{ 'span-all': Boolean(field.hint) }">
                <el-input v-model="form.pricing[field.key]" spellcheck="false" />
                <span v-if="field.hint" class="field-hint">{{ field.hint }}</span>
              </el-form-item>
            </div>
          </template>
        </el-collapse-item>

        <el-collapse-item name="quota">
          <template #title><span class="collapse-title">套餐额度<i>{{ quotaSummary(form) }}</i></span></template>
          <div class="field-grid">
            <el-form-item class="span-all" label="适配器"><el-select v-model="form.quota.adapter"><el-option v-for="item in QUOTA_ADAPTER_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
          </div>
          <template v-if="showQuotaDetail">
            <p v-if="quotaLocked" class="section-hint">该适配器的请求方式与鉴权由系统固定，以下字段无需填写。</p>
            <div v-else class="field-grid">
              <el-form-item class="span-all" label="额度查询路径"><el-input v-model="form.quota.path" placeholder="/api/monitor/usage/quota/limit" spellcheck="false" /></el-form-item>
              <el-form-item label="鉴权方式"><el-select v-model="form.quota.authType"><el-option v-for="item in AUTH_TYPE_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
              <el-form-item label="请求头名称"><el-input v-model="form.quota.headerName" placeholder="Authorization" spellcheck="false" /></el-form-item>
              <el-form-item class="span-all" label="请求头前缀"><el-input v-model="form.quota.headerPrefix" placeholder="Bearer " spellcheck="false" /></el-form-item>
            </div>
          </template>
        </el-collapse-item>

        <el-collapse-item name="media">
          <template #title><span class="collapse-title">音频与视频<i>{{ audioVideoSummary(form) }}</i></span></template>
          <div class="field-grid">
            <el-form-item label="音频接口形态"><el-select v-model="form.audio.adapter"><el-option v-for="item in AUDIO_ADAPTER_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
            <el-form-item label="视频接口形态"><el-select v-model="form.video.adapter"><el-option v-for="item in VIDEO_ADAPTER_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item>
          </div>
        </el-collapse-item>

        <el-collapse-item name="endpoints">
          <template #title><span class="collapse-title">上游端点<i>{{ endpointSummary(form) }}</i></span></template>
          <p class="section-hint">声明上游实际支持的接口路径；未声明的端点不会开放给客户端。</p>
          <div v-if="!endpoints.length" class="empty-endpoints">
            <span>当前未自定义端点，保存后使用内置端点表（chatCompletions、responses、images… ）。</span>
            <el-button size="small" :icon="Plus" @click="addEndpoint">添加端点</el-button>
          </div>
          <div v-for="name in endpoints" :key="name" class="endpoint-row">
            <div class="endpoint-line">
              <el-input :model-value="endpointName(name)" placeholder="端点名，如 chatCompletions" spellcheck="false" @update:model-value="editingNames[name] = String($event)" @change="commitEndpointName(name)" />
              <el-select v-model="form.endpoints[name].method" class="endpoint-method"><el-option v-for="item in METHOD_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select>
              <el-select v-model="form.endpoints[name].requestBody" class="endpoint-body"><el-option v-for="item in REQUEST_BODY_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select>
              <TableActionButton :icon="Trash2" label="删除端点" danger :size="15" @click="removeEndpoint(name)" />
            </div>
            <el-input v-model="form.endpoints[name].path" placeholder="路径，如 /chat/completions" spellcheck="false" />
            <div class="endpoint-line endpoint-line--auth">
              <el-select v-model="form.endpoints[name].authType"><el-option v-for="item in AUTH_TYPE_OPTIONS" :key="item.value" :label="item.label" :value="item.value" /></el-select>
              <el-input v-model="form.endpoints[name].headerName" placeholder="Authorization" spellcheck="false" />
              <el-input v-model="form.endpoints[name].headerPrefix" placeholder="Bearer " spellcheck="false" />
              <el-tooltip content="支持流式响应"><span class="stream-toggle"><el-switch v-model="form.endpoints[name].supportsStream" size="small" /></span></el-tooltip>
            </div>
          </div>
          <div class="endpoint-actions">
            <el-button size="small" :icon="Plus" @click="addEndpoint">添加端点</el-button>
            <el-button v-if="endpoints.length" size="small" @click="useBuiltinEndpoints">改用内置端点</el-button>
          </div>
        </el-collapse-item>
      </el-collapse>
    </el-form>

    <div v-else class="json-view">
      <div class="json-toolbar">
        <span v-if="jsonError" class="json-error">{{ jsonError }}</span>
        <span v-else class="json-ok">JSON 格式有效</span>
        <el-button size="small" :icon="Braces" :disabled="Boolean(jsonError)" @click="formatJson">格式化</el-button>
      </div>
      <el-input v-model="text" class="json-editor" type="textarea" :rows="22" :readonly="props.readonly" spellcheck="false" />
    </div>
  </div>
</template>

<style scoped>
.type-config-editor { display: grid; gap: 4px; }
.editor-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding-bottom: 6px; border-bottom: 1px solid #dce2e7; }
.editor-head__text { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.editor-head__text strong { color: #15202b; font-size: 13px; }
.editor-head__text span { color: #66717d; font-size: 11px; line-height: 1.5; }
.collapse-title { display: flex; align-items: baseline; gap: 8px; }
.collapse-title i { color: #7b8792; font-size: 11px; font-style: normal; }
.type-config-form :deep(.el-collapse) { border-top: 0; }
.type-config-form :deep(.el-collapse-item__header) { height: 44px; border-bottom-color: #e4e9ed; font-size: 12px; font-weight: 600; }
.type-config-form :deep(.el-collapse-item__content) { padding-bottom: 6px; }
.type-config-form :deep(.el-form-item) { margin-bottom: 10px; }
.type-config-form :deep(.el-form-item__label) { padding-bottom: 3px; color: #40505f; font-size: 12px; line-height: 1.4; }
.field-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
.field-grid .span-all { grid-column: 1 / -1; }
.field-hint, .section-hint { color: #7b8792; font-size: 11px; line-height: 1.5; }
.section-hint { margin: 0 0 10px; }
.setting-row { display: flex; min-height: 56px; align-items: center; justify-content: space-between; gap: 16px; border-top: 1px solid #e4e9ed; padding: 8px 0; }
.setting-row > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.setting-row strong { color: #15202b; font-size: 13px; }
.setting-row span { color: #66717d; font-size: 11px; line-height: 1.45; }
.setting-row :deep(.el-switch) { flex: 0 0 auto; }
.empty-endpoints { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; padding: 10px 12px; border: 1px dashed #cfd8df; border-radius: 6px; color: #66717d; background: #fbfcfd; font-size: 11px; line-height: 1.5; }
.endpoint-row { display: grid; gap: 8px; margin-bottom: 10px; padding: 10px; border: 1px solid #dce2e7; border-radius: 6px; background: #fbfcfd; }
.endpoint-line { display: grid; grid-template-columns: minmax(0, 1fr) 96px 116px 34px; align-items: center; gap: 8px; }
.endpoint-line--auth { grid-template-columns: minmax(0, 96px) minmax(0, 1fr) minmax(0, 1fr) 44px; }
.endpoint-line :deep(.el-select) { width: 100%; }
.stream-toggle { display: inline-flex; justify-content: center; }
.endpoint-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.json-view { display: grid; gap: 8px; padding-top: 10px; }
.json-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.json-toolbar .json-error { color: #c0392b; font-size: 11px; line-height: 1.5; }
.json-toolbar .json-ok { color: #16866f; font-size: 11px; }
.json-editor :deep(textarea) { min-height: 440px !important; font-family: 'JetBrains Mono', monospace; font-size: 12px; line-height: 1.55; }
@media (max-width: 600px) {
  .field-grid { grid-template-columns: 1fr; }
  .endpoint-line, .endpoint-line--auth { grid-template-columns: 1fr 1fr; }
  .editor-head { align-items: stretch; flex-direction: column; }
}
</style>
