<script setup lang="ts">
import { SlidersHorizontal } from '@lucide/vue'
import type { ChannelAdvancedConfig } from '../api/types'

defineProps<{ hasProxy: boolean; editing: boolean }>()
const emit = defineEmits<{ clearProxy: [] }>()
const config = defineModel<ChannelAdvancedConfig>('config', { required: true })
const proxyUrl = defineModel<string | undefined>('proxyUrl')

function setStoreAllowed(value: boolean | string | number) {
  config.value.blockStore = !Boolean(value)
}
</script>

<template>
  <section class="advanced-settings">
    <div class="advanced-heading"><SlidersHorizontal :size="17" /><strong>高级配置</strong></div>

    <div class="setting-group">
      <div class="setting-row">
        <div><strong>强制 OpenAI 格式</strong><span>补全兼容响应的 OpenAI 元数据</span></div>
        <el-switch v-model="config.forceOpenAIFormat" />
      </div>
      <div class="setting-row">
        <div><strong>思维到内容</strong><span>将 reasoning_content 转为 content 中的 &lt;think&gt; 标签</span></div>
        <el-switch v-model="config.reasoningToContent" />
      </div>
      <div class="setting-row">
        <div><strong>智能协议转换</strong><span>上游不支持当前端点时，在 Chat Completions 与 Responses 间自动适配</span></div>
        <el-switch v-model="config.enableProtocolConversion" />
      </div>
      <div class="setting-row">
        <div><strong>透传请求体</strong><span>默认关闭，仅转发已支持字段；下方字段开关仍优先执行</span></div>
        <el-switch v-model="config.passthroughRequestBody" />
      </div>
    </div>

    <div class="proxy-field">
      <div class="field-label"><strong>代理地址</strong><el-button v-if="editing && hasProxy" text size="small" @click="emit('clearProxy')">清除已保存代理</el-button></div>
      <el-input v-model="proxyUrl" type="password" show-password :placeholder="editing && hasProxy ? '已配置；留空保持不变' : 'socks5://user:pass@host:port'" autocomplete="new-password" />
      <span>此渠道的网络代理，仅支持 SOCKS5。</span>
    </div>

    <div class="prompt-field">
      <strong>系统提示词</strong>
      <el-input v-model="config.systemPrompt" type="textarea" :rows="4" maxlength="16384" show-word-limit placeholder="输入渠道默认系统提示词（用户提示词优先）" />
      <div class="setting-row compact">
        <div><strong>连接系统提示词</strong><span>将渠道提示置于用户系统提示词之前</span></div>
        <el-switch v-model="config.appendSystemPrompt" :disabled="!config.systemPrompt" />
      </div>
    </div>

    <div class="field-controls">
      <div class="section-caption">字段透传控制</div>
      <div class="setting-row"><div><strong>允许 service_tier</strong><span>将 service_tier 字段发送到上游</span></div><el-switch v-model="config.allowServiceTier" /></div>
      <div class="setting-row"><div><strong>允许 store</strong><span>默认阻断 store，保护请求一致性</span></div><el-switch :model-value="!config.blockStore" @update:model-value="setStoreAllowed" /></div>
      <div class="setting-row"><div><strong>允许 safety_identifier</strong><span>将 safety_identifier 字段发送到上游</span></div><el-switch v-model="config.allowSafetyIdentifier" /></div>
      <div class="setting-row"><div><strong>允许 include</strong><span>将 include 字段发送到上游</span></div><el-switch v-model="config.allowInclude" /></div>
      <div class="setting-row"><div><strong>允许 inference_geo</strong><span>将 inference_geo 字段发送到上游</span></div><el-switch v-model="config.allowInferenceGeo" /></div>
    </div>
  </section>
</template>

<style scoped>
.advanced-settings { margin-top: 20px; border-top: 1px solid #dce2e7; }.advanced-heading { display: flex; align-items: center; gap: 8px; padding: 16px 0 10px; color: #15202b; }.advanced-heading svg { color: #1677ff; }.advanced-heading strong, .setting-row strong, .proxy-field strong, .prompt-field > strong { font-size: 13px; }.setting-group, .field-controls { border-top: 1px solid #dce2e7; }.setting-row { display: flex; min-height: 61px; align-items: center; justify-content: space-between; gap: 16px; border-bottom: 1px solid #dce2e7; padding: 8px 0; }.setting-row > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.setting-row span, .proxy-field > span { color: #66717d; font-size: 11px; line-height: 1.45; }.setting-row :deep(.el-switch) { flex: 0 0 auto; }.proxy-field, .prompt-field { display: flex; flex-direction: column; gap: 8px; padding: 16px 0; border-bottom: 1px solid #dce2e7; }.field-label { display: flex; align-items: center; justify-content: space-between; gap: 12px; }.field-label :deep(.el-button) { height: auto; padding: 0; }.compact { min-height: 52px; margin-top: 4px; border-bottom: 0; }.section-caption { padding: 14px 0 5px; color: #40505f; font-size: 12px; font-weight: 600; }@media (max-width: 480px) { .setting-row { align-items: flex-start; padding: 12px 0; }.setting-row :deep(.el-switch) { margin-top: 4px; } }
</style>
