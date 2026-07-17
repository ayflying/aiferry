<script setup lang="ts">
import { computed } from 'vue'
import type { UsageLog } from '../api/types'
import { formatCost, formatNumber, formatTime } from '../lib/format'

const props = defineProps<{ modelValue: boolean; usage?: UsageLog }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const isSuccess = computed(() => !!props.usage && props.usage.httpStatus >= 200 && props.usage.httpStatus < 300)
const resultMessage = computed(() => {
  if (!props.usage) return ''
  return props.usage.errorMessage || (isSuccess.value ? '模型响应正常' : '未返回错误详情')
})
</script>

<template>
  <el-dialog v-model="visible" title="调用详情" width="720px" class="usage-detail-dialog" destroy-on-close>
    <template v-if="usage">
      <div class="detail-summary">
        <div><span>估算成本</span><strong>{{ formatCost(usage.estimatedCost) }}</strong></div>
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
          <el-descriptions-item label="状态"><el-tag :type="isSuccess ? 'success' : 'danger'" effect="plain" size="small">{{ usage.httpStatus }}</el-tag></el-descriptions-item>
          <el-descriptions-item label="请求模型">{{ usage.requestedModel }}</el-descriptions-item>
          <el-descriptions-item label="上游模型">{{ usage.upstreamModel || '—' }}</el-descriptions-item>
          <el-descriptions-item label="渠道">{{ usage.channelName || '—' }}</el-descriptions-item>
          <el-descriptions-item label="密钥">{{ usage.apiKeyName || '—' }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-section">
        <h3>费用</h3>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="估算成本"><strong class="cost-value">{{ formatCost(usage.estimatedCost) }}</strong></el-descriptions-item>
          <el-descriptions-item label="定价状态">{{ usage.estimatedCost == null ? '模型尚未定价' : '按模型价格估算' }}</el-descriptions-item>
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
:deep(.usage-detail-dialog) { width: min(720px, calc(100vw - 32px)) !important; }.detail-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin-bottom: 20px; border: 1px solid #dce2e7; }.detail-summary div { display: flex; min-height: 66px; flex-direction: column; justify-content: center; gap: 5px; padding: 0 14px; border-right: 1px solid #dce2e7; }.detail-summary div:last-child { border-right: 0; }.detail-summary span, .result-meta { color: #66717d; font-size: 11px; }.detail-summary strong, .token-metrics strong, .cost-value { color: #15202b; font-family: 'JetBrains Mono', monospace; font-size: 13px; }.detail-section { padding: 18px 0; border-top: 1px solid #dce2e7; }.detail-section h3 { margin: 0 0 12px; color: #15202b; font-size: 13px; }.mono { font-family: 'JetBrains Mono', monospace; }.detail-value { display: inline-block; max-width: 100%; overflow-wrap: anywhere; }.token-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid #dce2e7; }.token-metrics div { display: flex; min-height: 64px; flex-direction: column; justify-content: center; gap: 5px; padding: 0 12px; border-right: 1px solid #dce2e7; }.token-metrics div:last-child { border-right: 0; }.token-metrics span { color: #66717d; font-size: 11px; }.result-section { padding-bottom: 0; }.result-message { margin: 0 0 6px; color: #40505f; overflow-wrap: anywhere; }.danger-result { color: #d14343; }@media (max-width: 720px) { .detail-summary { grid-template-columns: 1fr; }.detail-summary div { min-height: 58px; border-right: 0; border-bottom: 1px solid #dce2e7; }.detail-summary div:last-child { border-bottom: 0; }.token-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }.token-metrics div:nth-child(2) { border-right: 0; }.token-metrics div:nth-child(-n + 2) { border-bottom: 1px solid #dce2e7; } }@media (max-width: 480px) { .token-metrics { grid-template-columns: 1fr; }.token-metrics div { border-right: 0; border-bottom: 1px solid #dce2e7; }.token-metrics div:last-child { border-bottom: 0; } }
</style>
