<script setup lang="ts">
import { computed } from 'vue'
import type { PriceRule, PublicModel, UsageLog } from '../api/types'
import { formatCost, formatNumber, formatTime } from '../lib/format'
import { formatIPLocation } from '../lib/ip-location'

const props = defineProps<{
  modelValue: boolean
  usage?: UsageLog
  model?: PublicModel
  priceRules: PriceRule[]
  priceLoading: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const tokenPrices = [
  ['输入价格', 'inputPrice'],
  ['补全价格', 'outputPrice'],
  ['缓存读取价格', 'cachedInputPrice'],
  ['缓存写入价格', 'cacheWritePrice'],
  ['图像输入价格', 'imageInputPrice'],
  ['音频输入价格', 'audioInputPrice'],
  ['音频输出价格', 'audioOutputPrice'],
] as const

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const isSuccess = computed(() => !!props.usage && props.usage.httpStatus >= 200 && props.usage.httpStatus < 300)
const resultMessage = computed(() => {
  if (!props.usage) return ''
  return props.usage.errorMessage || (isSuccess.value ? '模型响应正常' : '未返回错误详情')
})
const billingModeLabel = computed(() => {
	if (props.model?.billingMode === 'request') return '按次'
	if (props.model?.billingMode === 'rules') return '高级规则'
	return '按 Token'
})
const activePriceRules = computed(() => props.priceRules.filter((rule) => rule.status === 1))
const configuredTokenPrices = computed(() => tokenPrices.filter(([, field]) => isConfiguredPrice(props.model?.[field])))

function tokenPrice(field: typeof tokenPrices[number][1]) {
	const value = props.model?.[field]
	return isConfiguredPrice(value) ? `${formatCost(value)} / 1M Token` : ''
}

function requestPrice() {
  const value = props.model?.requestPrice
  return value === undefined ? '—' : `${formatCost(value)} / 请求`
}

function ruleRates(rule: PriceRule) {
  const labels: Record<string, string> = {
    inputPerMillion: '输入', cachedInputPerMillion: '缓存读取', cacheWritePerMillion: '缓存写入',
    outputPerMillion: '补全', imageInputPerMillion: '图像输入', audioInputPerMillion: '音频输入',
    audioOutputPerMillion: '音频输出', request: '按次',
  }
  const rates = Object.entries(rule.rates).filter(([, value]) => typeof value === 'number' && Number.isFinite(value))
	return rates.map(([name, value]) => `${labels[name] || name} ${formatCost(value, rule.currency)}`).join(' · ') || '未配置价格'
}

function isConfiguredPrice(value: unknown): value is number {
	return typeof value === 'number' && Number.isFinite(value)
}
</script>

<template>
  <el-dialog v-model="visible" title="调用详情" width="720px" class="usage-detail-dialog" destroy-on-close>
    <template v-if="usage">
      <div class="detail-summary">
        <div><span>计费方式</span><strong>{{ billingModeLabel }}</strong></div>
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
          <el-descriptions-item label="上游模型">{{ usage.upstreamModel || '—' }}</el-descriptions-item>
          <el-descriptions-item label="渠道">{{ usage.channelName || '—' }}</el-descriptions-item>
          <el-descriptions-item label="密钥">{{ usage.apiKeyName || '—' }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-section">
        <h3>模型价格</h3>
        <template v-if="priceLoading">
          <p class="empty-price">正在读取当前模型价格</p>
        </template>
        <template v-else-if="!model">
          <p class="empty-price">当前模型未配置价格</p>
        </template>
		<template v-else-if="model.billingMode === 'token'">
		  <div v-if="configuredTokenPrices.length" class="price-metrics">
			<div v-for="([label, field]) in configuredTokenPrices" :key="field"><span>{{ label }}</span><strong>{{ tokenPrice(field) }}</strong></div>
		  </div>
		  <p v-else class="empty-price">当前模型未配置 Token 价格</p>
		</template>
        <el-descriptions v-else-if="model.billingMode === 'request'" :column="2" border size="small">
          <el-descriptions-item label="固定价格"><strong class="cost-value">{{ requestPrice() }}</strong></el-descriptions-item>
          <el-descriptions-item label="计费说明">每次请求固定扣费，不考虑 Token 数</el-descriptions-item>
        </el-descriptions>
        <el-descriptions v-else :column="1" border size="small">
          <el-descriptions-item label="生效规则">{{ activePriceRules.length }} 条</el-descriptions-item>
          <el-descriptions-item v-for="rule in activePriceRules" :key="rule.id" :label="rule.name">
            <span class="detail-value">{{ ruleRates(rule) }}</span>
          </el-descriptions-item>
          <el-descriptions-item v-if="!activePriceRules.length" label="高级规则">未配置生效规则</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-section">
        <h3>Token 使用</h3>
        <div class="token-metrics">
          <div><span>输入</span><strong>{{ formatNumber(usage.inputTokens) }}</strong></div>
          <div><span>缓存读取</span><strong>{{ formatNumber(usage.cachedInputTokens) }}</strong></div>
          <div><span>输出</span><strong>{{ formatNumber(usage.outputTokens) }}</strong></div>
          <div><span>总计</span><strong>{{ formatNumber(usage.totalTokens) }}</strong></div>
        </div>
      </section>

      <section class="detail-section result-section">
        <h3>处理结果</h3>
        <p class="result-message" :class="{ 'danger-result': !isSuccess }">{{ resultMessage }}</p>
        <span class="result-meta">首包 {{ usage.firstTokenMs ?? '—' }} ms · 上游尝试 {{ usage.attempts }} 次</span>
      </section>
    </template>
  </el-dialog>
</template>

<style scoped>
:deep(.usage-detail-dialog) { width: min(720px, calc(100vw - 32px)) !important; }.detail-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin-bottom: 20px; border: 1px solid #dce2e7; }.detail-summary div { display: flex; min-height: 66px; flex-direction: column; justify-content: center; gap: 5px; padding: 0 14px; border-right: 1px solid #dce2e7; }.detail-summary div:last-child { border-right: 0; }.detail-summary span, .result-meta, .price-metrics span { color: #66717d; font-size: 11px; }.detail-summary strong, .token-metrics strong, .price-metrics strong, .cost-value { color: #15202b; font-family: 'JetBrains Mono', monospace; font-size: 13px; }.detail-section { padding: 18px 0; border-top: 1px solid #dce2e7; }.detail-section h3 { margin: 0 0 12px; color: #15202b; font-size: 13px; }.mono { font-family: 'JetBrains Mono', monospace; }.detail-value { display: inline-block; max-width: 100%; overflow-wrap: anywhere; }.token-metrics, .price-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid #dce2e7; }.token-metrics div, .price-metrics div { display: flex; min-height: 64px; flex-direction: column; justify-content: center; gap: 5px; padding: 0 12px; border-right: 1px solid #dce2e7; }.token-metrics div:last-child, .price-metrics div:last-child { border-right: 0; }.price-metrics div:nth-child(4n) { border-right: 0; }.price-metrics div:nth-child(-n + 4) { border-bottom: 1px solid #dce2e7; }.result-section { padding-bottom: 0; }.result-message { margin: 0 0 6px; color: #40505f; overflow-wrap: anywhere; }.danger-result { color: #d14343; }.empty-price { margin: 0; color: #66717d; font-size: 13px; }@media (max-width: 720px) { .detail-summary { grid-template-columns: 1fr; }.detail-summary div { min-height: 58px; border-right: 0; border-bottom: 1px solid #dce2e7; }.detail-summary div:last-child { border-bottom: 0; }.token-metrics, .price-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }.token-metrics div:nth-child(2), .price-metrics div:nth-child(2n) { border-right: 0; }.token-metrics div:nth-child(-n + 2), .price-metrics div:nth-child(-n + 6) { border-bottom: 1px solid #dce2e7; } }@media (max-width: 480px) { .token-metrics, .price-metrics { grid-template-columns: 1fr; }.token-metrics div, .price-metrics div { border-right: 0; border-bottom: 1px solid #dce2e7; }.token-metrics div:last-child, .price-metrics div:last-child { border-bottom: 0; } }
</style>
