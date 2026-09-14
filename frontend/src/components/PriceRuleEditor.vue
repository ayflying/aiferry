<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import type { TimeWindow } from '../api/types'
import type { PriceRuleDraft } from '../lib/model-pricing'
import { createTimeWindow, toTimeWindow } from '../lib/time-window'
import TimeWindowEditor from './TimeWindowEditor.vue'

type TokenDimension = 'inputTokens' | 'outputTokens' | 'totalTokens'
type TokenBound = { atLeast?: number; atMost?: number }

const props = withDefaults(defineProps<{ modelValue: PriceRuleDraft; saving?: boolean }>(), { saving: false })
const emit = defineEmits<{ (event: 'update:modelValue', value: PriceRuleDraft): void; (event: 'submit'): void }>()

const tokenDimensions: Array<{ key: TokenDimension; label: string }> = [
  { key: 'inputTokens', label: '输入 Token' },
  { key: 'outputTokens', label: '输出 Token' },
  { key: 'totalTokens', label: '总 Token' },
]

// 字段名与后端 internal/logic/usage/pricing.go 的 rulePriceRates 保持一致。
const rateFields = [
  { key: 'inputPerMillion', label: '输入' },
  { key: 'cachedInputPerMillion', label: '缓存读取' },
  { key: 'cacheWritePerMillion', label: '缓存写入' },
  { key: 'outputPerMillion', label: '输出' },
  { key: 'imageInputPerMillion', label: '图像输入' },
  { key: 'audioInputPerMillion', label: '音频输入' },
  { key: 'audioOutputPerMillion', label: '音频输出' },
  { key: 'request', label: '每次请求' },
]

const endpointOptions = ['/chat/completions', '/responses', '/embeddings', '/images/generations', '/images/edits', '/audio/speech', '/audio/transcriptions']
const currencyOptions = ['USD', 'CNY']

const name = ref('')
const priority = ref(100)
const currency = ref('USD')
const endpoint = ref('')
const timeEnabled = ref(false)
const timeWindow = ref<TimeWindow>(createTimeWindow())
const tokenBounds = reactive<Record<TokenDimension, TokenBound>>({ inputTokens: {}, outputTokens: {}, totalTokens: {} })
const rates = reactive<Record<string, number | undefined>>({})

function buildConditions(): Record<string, unknown> {
  const conditions: Record<string, unknown> = {}
  const trimmedEndpoint = endpoint.value.trim()
  if (trimmedEndpoint) conditions.endpoint = trimmedEndpoint
  for (const { key } of tokenDimensions) {
    const bound = tokenBounds[key]
    if (typeof bound.atLeast === 'number') conditions[`${key}AtLeast`] = bound.atLeast
    if (typeof bound.atMost === 'number') conditions[`${key}AtMost`] = bound.atMost
  }
  if (timeEnabled.value) {
    const window = timeWindow.value
    const time: Record<string, unknown> = { tz: window.tz?.trim() || 'Asia/Shanghai' }
    if (window.weekdays?.length) time.weekdays = [...window.weekdays]
    const pairs = (window.ranges ?? []).filter((item) => item[0] && item[1])
    if (pairs.length) time.ranges = pairs
    conditions.time = time
  }
  return conditions
}

function buildRates(): Record<string, number> {
  const result: Record<string, number> = {}
  for (const { key } of rateFields) {
    const value = rates[key]
    if (typeof value === 'number' && Number.isFinite(value)) result[key] = value
  }
  return result
}

function buildValue(): PriceRuleDraft {
  return {
    name: name.value.trim(),
    priority: priority.value,
    currency: currency.value.trim() || 'USD',
    conditions: buildConditions(),
    rates: buildRates(),
  }
}

function hydrate(value?: PriceRuleDraft) {
  name.value = typeof value?.name === 'string' ? value.name : ''
  priority.value = typeof value?.priority === 'number' ? value.priority : 100
  currency.value = typeof value?.currency === 'string' && value.currency.trim() ? value.currency : 'USD'
  const conditions = value?.conditions && typeof value.conditions === 'object' ? value.conditions : {}
  endpoint.value = typeof conditions['endpoint'] === 'string' ? conditions['endpoint'] : ''
  for (const { key } of tokenDimensions) {
    const atLeast = conditions[`${key}AtLeast`]
    const atMost = conditions[`${key}AtMost`]
    tokenBounds[key] = {
      atLeast: typeof atLeast === 'number' ? atLeast : undefined,
      atMost: typeof atMost === 'number' ? atMost : undefined,
    }
  }
  const block = conditions['time']
  const time = block && typeof block === 'object' && !Array.isArray(block) ? (block as Record<string, unknown>) : null
  timeEnabled.value = time !== null
  timezone.value = typeof time?.tz === 'string' && time.tz ? time.tz : 'Asia/Shanghai'
  weekdays.value = Array.isArray(time?.weekdays) ? time.weekdays.filter((item): item is number => typeof item === 'number') : []
  ranges.value = Array.isArray(time?.ranges)
    ? time.ranges
        .filter((item): item is unknown[] => Array.isArray(item) && item.length === 2)
        .map((item) => ({ start: String(item[0]), end: String(item[1]) }))
    : []
  const incoming = value?.rates && typeof value.rates === 'object' ? (value.rates as Record<string, unknown>) : {}
  for (const { key } of rateFields) {
    const rate = incoming[key]
    rates[key] = typeof rate === 'number' && Number.isFinite(rate) ? rate : undefined
  }
}

// syncing 期间忽略子表单回写，避免「外部回填 → 重新 emit → 再回填」的自激循环。
let syncing = false
watch(() => props.modelValue, (value) => {
  if (syncing) return
  syncing = true
  hydrate(value)
  void nextTick(() => { syncing = false })
}, { immediate: true, deep: true })

watch([name, priority, currency, endpoint, timeEnabled, timeWindow, tokenBounds, rates], () => {
  if (syncing) return
  emit('update:modelValue', buildValue())
}, { deep: true })

const generatedConditions = computed(() => JSON.stringify(buildConditions(), null, 2))
</script>

<template>
  <div class="price-rule-editor">
    <el-input v-model="name" placeholder="规则名称，例如 Chat 高峰价" maxlength="96" show-word-limit />
    <div class="editor-grid">
      <div class="editor-field"><span>优先级</span><el-input-number v-model="priority" :min="-999" :max="999" controls-position="right" /></div>
      <div class="editor-field"><span>计价货币</span><el-select v-model="currency" filterable allow-create default-first-option placeholder="USD"><el-option v-for="item in currencyOptions" :key="item" :label="item" :value="item" /></el-select></div>
    </div>

    <section class="editor-block">
      <header class="editor-block__head">
        <strong>生效时段</strong>
        <el-switch v-model="timeEnabled" />
      </header>
      <p class="editor-hint">关闭时该规则全天生效；开启后可限定星期与每日区间。</p>
      <TimeWindowEditor v-if="timeEnabled" v-model="timeWindow" />
    </section>

    <section class="editor-block">
      <header class="editor-block__head"><strong>生效端点</strong></header>
      <el-select v-model="endpoint" clearable filterable allow-create default-first-option placeholder="留空表示不限端点">
        <el-option v-for="item in endpointOptions" :key="item" :label="item" :value="item" />
      </el-select>
    </section>

    <section class="editor-block">
      <header class="editor-block__head"><strong>Token 区间</strong></header>
      <p class="editor-hint">留空表示不限；命中任一区间的判断对输入、输出和总量分别生效。</p>
      <div class="token-row" v-for="item in tokenDimensions" :key="item.key">
        <span class="editor-hint">{{ item.label }}</span>
        <el-input-number v-model="tokenBounds[item.key].atLeast" :min="0" :controls="false" placeholder="下限" />
        <el-input-number v-model="tokenBounds[item.key].atMost" :min="0" :controls="false" placeholder="上限" />
      </div>
    </section>

    <section class="editor-block">
      <header class="editor-block__head"><strong>费率</strong></header>
      <p class="editor-hint">每百万 Token 单价，留空表示该维度不计费；「每次请求」为固定费用。</p>
      <div class="editor-grid">
        <div class="editor-field" v-for="item in rateFields" :key="item.key">
          <span>{{ item.label }}</span>
          <el-input-number v-model="rates[item.key]" :min="0" :precision="6" :controls="false" placeholder="未定价" />
        </div>
      </div>
    </section>

    <el-collapse class="editor-preview">
      <el-collapse-item title="查看生成的匹配条件" name="conditions"><pre>{{ generatedConditions }}</pre></el-collapse-item>
    </el-collapse>

    <el-button type="primary" :loading="saving" @click="emit('submit')">添加人工规则</el-button>
  </div>
</template>

<style scoped>
.price-rule-editor { display: grid; gap: 12px; margin-top: 12px; padding-top: 12px; border-top: 1px solid #dce2e7; }.price-rule-editor :deep(.el-input-number) { width: 100%; }.editor-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }.editor-field { display: flex; flex-direction: column; gap: 4px; }.editor-field > span { color: #66717d; font-size: 11px; }.editor-block { display: grid; gap: 8px; padding: 11px; border: 1px solid #dce2e7; border-radius: 6px; background: #fbfcfd; }.editor-block__head { display: flex; align-items: center; gap: 8px; }.editor-block__head strong { color: #33404c; font-size: 12px; }.editor-block__head .el-switch { margin-left: auto; }.editor-hint { margin: 0; color: #66717d; font-size: 11px; line-height: 1.5; }.timezone-select { width: 100%; }.weekday-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.range-row { display: grid; grid-template-columns: 1fr auto 1fr auto; align-items: center; gap: 6px; }.range-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.token-row { display: grid; grid-template-columns: 88px 1fr 1fr; align-items: center; gap: 8px; }.editor-preview { border-top: 0; }.editor-preview :deep(.el-collapse-item__header) { font-size: 12px; }.editor-preview pre { margin: 0; overflow-x: auto; color: #4b5763; font-family: 'JetBrains Mono', monospace; font-size: 11px; line-height: 1.6; }
</style>
