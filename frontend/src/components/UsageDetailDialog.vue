<script setup lang="ts">
import { computed } from 'vue'
import type { BillingItem, UsageLog } from '../api/types'
import { formatNumber, formatPreciseCost, formatReasoningEffort, formatTime } from '../lib/format'
import { formatIPLocation } from '../lib/ip-location'

const props = defineProps<{
  modelValue: boolean
  usage?: UsageLog
}>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const itemLabels: Record<BillingItem['type'], string> = {
  input: '输入 Token',
  cached_input: '缓存读取 Token',
  cache_write: '缓存写入 Token',
  output: '输出 Token',
  image_input: '图像输入 Token',
  audio_input: '音频输入 Token',
  audio_output: '音频输出 Token',
  request: '请求固定费用',
  rounding: '结算取整',
}
const priceSourceLabels: Record<string, string> = {
  input: '输入单价', cached_input: '缓存读取单价', cache_write: '缓存写入单价', output: '输出单价',
  image_input: '图像输入单价', audio_input: '音频输入单价', audio_output: '音频输出单价',
  request: '请求单价', settlement: '结算取整',
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const isSuccess = computed(() => !!props.usage && props.usage.httpStatus >= 200 && props.usage.httpStatus < 300)
const billingDetails = computed(() => props.usage?.billingDetails)
const billingItems = computed(() => billingDetails.value?.items ?? [])
const resultMessage = computed(() => {
  if (!props.usage) return ''
  return props.usage.errorMessage || (isSuccess.value ? '模型响应正常' : '未返回错误详情')
})
const billingModeLabel = computed(() => {
  switch (billingDetails.value?.billingMode) {
    case 'request': return '按次'
    case 'rules': return '高级规则'
    case 'token': return '按 Token'
    default: return '未保存'
  }
})
const billingSourceLabel = computed(() => billingDetails.value?.reconstructed ? '历史价格快照复原' : '调用时价格快照')

function itemUnitPrice(item: BillingItem) {
  const currency = billingDetails.value?.currency
  if (item.unit === 'per_request') return `${formatPreciseCost(item.unitPrice, currency)} / 次`
  if (item.unit === 'settlement') return '结算精度调整'
  return `${formatPreciseCost(item.unitPrice, currency)} / 1M Token`
}

function itemPriceSource(item: BillingItem) {
  const source = priceSourceLabels[item.priceSource || '']
  return source && item.priceSource !== item.type ? `采用${source}` : ''
}

function itemLabel(item: BillingItem) {
  return itemLabels[item.type] || item.type
}

function billingSummary() {
  const details = billingDetails.value
  return ['总计', '', details ? formatPreciseCost(details.total, details.currency) : '']
}
</script>

<template>
  <el-dialog v-model="visible" title="调用详情" width="760px" class="usage-detail-dialog" :style="{ height: 'min(720px, calc(100vh - 32px))', maxHeight: 'calc(100vh - 32px)', margin: '16px auto', display: 'flex', flexDirection: 'column', overflow: 'hidden' }" destroy-on-close>
    <template v-if="usage">
      <div class="detail-summary">
        <div><span>模型价格</span><strong>{{ billingDetails ? billingSourceLabel : '未保存价格快照' }}</strong></div>
        <div><span>总 Token</span><strong>{{ formatNumber(usage.totalTokens) }}</strong></div>
        <div><span>响应耗时</span><strong>{{ usage.durationMs }} ms</strong></div>
      </div>

      <section class="detail-section">
        <h3>请求信息</h3>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="请求时间">{{ formatTime(usage.createdAt) }}</el-descriptions-item>
          <el-descriptions-item label="调用方式">{{ usage.isStream ? '流式响应' : '普通响应' }}</el-descriptions-item>
          <el-descriptions-item label="请求 ID" :span="2"><span class="mono detail-value">{{ usage.requestId }}</span></el-descriptions-item>
          <el-descriptions-item label="接口"><span class="mono">{{ usage.endpoint }}</span></el-descriptions-item>
          <el-descriptions-item label="上游接口"><span class="mono">{{ usage.upstreamEndpoint || usage.endpoint }}</span></el-descriptions-item>
          <el-descriptions-item label="协议转换" :span="2">{{ usage.protocolConversion ? `${usage.endpoint} → ${usage.upstreamEndpoint || usage.endpoint}` : '未转换' }}</el-descriptions-item>
          <el-descriptions-item label="客户端 IP"><span class="mono">{{ usage.clientIp || '—' }}</span></el-descriptions-item>
          <el-descriptions-item label="归属地"><span class="detail-value">{{ formatIPLocation(usage.ipLocation) }}</span></el-descriptions-item>
          <el-descriptions-item label="状态"><el-tag :type="isSuccess ? 'success' : 'danger'" effect="plain" size="small">{{ usage.httpStatus }}</el-tag></el-descriptions-item>
          <el-descriptions-item label="请求模型">{{ usage.requestedModel }}</el-descriptions-item>
          <el-descriptions-item label="推理强度">{{ formatReasoningEffort(usage.reasoningEffort) }}</el-descriptions-item>
          <el-descriptions-item label="上游模型">{{ usage.upstreamModel || '—' }}</el-descriptions-item>
          <el-descriptions-item label="渠道">{{ usage.channelName || '—' }}</el-descriptions-item>
          <el-descriptions-item label="密钥">{{ usage.apiKeyName || '—' }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-section">
        <h3>模型价格明细</h3>
        <template v-if="billingDetails">
          <el-descriptions :column="2" border size="small" class="billing-summary">
            <el-descriptions-item label="计费方式">{{ billingModeLabel }}</el-descriptions-item>
            <el-descriptions-item label="币种">{{ billingDetails.currency }}</el-descriptions-item>
            <el-descriptions-item label="价格来源">{{ billingSourceLabel }}</el-descriptions-item>
            <el-descriptions-item v-if="billingDetails.rule" label="命中规则">{{ billingDetails.rule.name || `规则 #${billingDetails.rule.id}` }} · P{{ billingDetails.rule.priority }} · {{ billingDetails.rule.source === 'sync' ? '上游同步' : '人工规则' }}</el-descriptions-item>
            <el-descriptions-item v-if="billingDetails.rule" label="规则条件"><span class="detail-value mono">{{ billingDetails.rule.conditions }}</span></el-descriptions-item>
            <el-descriptions-item label="模型价格小计"><strong class="cost-value">{{ formatPreciseCost(billingDetails.subtotal, billingDetails.currency) }}</strong></el-descriptions-item>
          </el-descriptions>
          <el-table :data="billingItems" border size="small" table-layout="fixed" class="billing-items" show-summary :summary-method="billingSummary">
            <el-table-column label="计价项" min-width="132"><template #default="{ row }"><div class="billing-item-name"><strong>{{ itemLabel(row) }}</strong><small v-if="itemPriceSource(row)">{{ itemPriceSource(row) }}</small></div></template></el-table-column>
            <el-table-column label="模型单价" min-width="174"><template #default="{ row }"><span class="formula-value">{{ itemUnitPrice(row) }}</span></template></el-table-column>
            <el-table-column label="预计费用" width="138" align="right"><template #default="{ row }"><strong class="cost-value">{{ formatPreciseCost(row.amount, billingDetails.currency) }}</strong></template></el-table-column>
          </el-table>
        </template>
        <p v-else class="empty-billing">该历史记录未保存模型价格快照，无法展示当次调用的模型价格。</p>
      </section>

      <section class="detail-section result-section">
        <h3>{{ isSuccess ? '处理结果' : '失败日志' }}</h3>
        <p v-if="isSuccess" class="result-message">{{ resultMessage }}</p>
        <pre v-else class="failure-log">{{ resultMessage }}</pre>
        <span class="result-meta">首包 {{ usage.firstTokenMs ?? '—' }} ms · 上游尝试 {{ usage.attempts }} 次</span>
      </section>
    </template>
  </el-dialog>
</template>

<style scoped>
:global(.usage-detail-dialog) { display: flex; width: min(760px, calc(100vw - 32px)) !important; flex-direction: column; overflow: hidden; }
:global(.usage-detail-dialog .el-dialog__header), :global(.usage-detail-dialog .el-dialog__footer) { flex: 0 0 auto; }
:global(.usage-detail-dialog .el-dialog__body) { box-sizing: border-box; min-height: 0; flex: 1 1 auto; overflow-y: scroll !important; overscroll-behavior: contain; scrollbar-gutter: stable; }
:global(.usage-detail-dialog .el-dialog__body::-webkit-scrollbar) { width: 10px; }
:global(.usage-detail-dialog .el-dialog__body::-webkit-scrollbar-thumb) { background: #aebac5; border: 3px solid transparent; background-clip: content-box; }
:global(.usage-detail-dialog .el-dialog__body::-webkit-scrollbar-track) { background: #f4f6f8; }
.detail-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin-bottom: 20px; border: 1px solid #dce2e7; }
.detail-summary div { display: flex; min-height: 66px; flex-direction: column; justify-content: center; gap: 5px; padding: 0 14px; border-right: 1px solid #dce2e7; }
.detail-summary div:last-child { border-right: 0; }
.detail-summary span, .result-meta, .billing-item-name small { color: #66717d; font-size: 11px; }
.detail-summary strong, .cost-value, .formula-value { color: #15202b; font-family: 'JetBrains Mono', monospace; font-size: 13px; }
.detail-section { padding: 18px 0; border-top: 1px solid #dce2e7; }
.detail-section h3 { margin: 0 0 12px; color: #15202b; font-size: 13px; }
.mono { font-family: 'JetBrains Mono', monospace; }
.detail-value { display: inline-block; max-width: 100%; overflow-wrap: anywhere; }
.billing-summary { margin-bottom: 12px; }
.billing-items { width: 100%; }
.billing-items :deep(.el-table__footer-wrapper td:last-child) { color: #15202b; font-family: 'JetBrains Mono', monospace; font-weight: 700; }
.billing-item-name { display: flex; flex-direction: column; gap: 3px; }
.formula-value { display: inline-block; color: #40505f; font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.empty-billing { margin: 0; color: #66717d; font-size: 13px; }
.result-section { padding-bottom: 0; }
.result-message { margin: 0 0 6px; color: #40505f; overflow-wrap: anywhere; }
.failure-log { max-height: 300px; margin: 0 0 8px; padding: 12px; overflow: auto; color: #9f2f2f; background: #fff5f5; border: 1px solid #f1cccc; font-family: 'JetBrains Mono', monospace; font-size: 12px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 720px) {
  :global(.usage-detail-dialog) { height: calc(100vh - 24px) !important; max-height: calc(100vh - 24px) !important; margin: 12px auto !important; }
  .detail-summary { grid-template-columns: 1fr; }
  .detail-summary div { min-height: 58px; border-right: 0; border-bottom: 1px solid #dce2e7; }
  .detail-summary div:last-child { border-bottom: 0; }
}
</style>
