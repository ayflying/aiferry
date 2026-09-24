<script setup lang="ts">
import { computed } from 'vue'
import { SlidersHorizontal } from '@lucide/vue'
import type { ChannelAdvancedConfig } from '../api/types'

defineProps<{ testingProxy: boolean }>()
const emit = defineEmits<{ testProxy: [] }>()
const config = defineModel<ChannelAdvancedConfig>('config', { required: true })
const proxyUrl = defineModel<string | undefined>('proxyUrl')

// 渠道级协议转换开关是三态的：未设置时跟随系统设置，所以要区分 null 与 false。
const protocolConversion = computed({
  get: () => (config.value.protocolConversion === true ? 'on' : config.value.protocolConversion === false ? 'off' : 'inherit'),
  set: (value: string) => {
    config.value.protocolConversion = value === 'inherit' ? null : value === 'on'
  },
})

function setStoreAllowed(value: boolean | string | number) {
  config.value.blockStore = !Boolean(value)
}

// 缓存字段处置是三态的：默认由系统按用户与凭据生成稳定缓存键；上游对未知字段
// 做白名单校验时选「不下发」（既不加字段，也不放客户端的字段漏过去）；需要客户端
// 自行控制缓存时选「跟随客户端」，写回旧的透传开关以保持历史配置兼容。
const promptCacheMode = computed({
  get: () => {
    if (config.value.passthroughPromptCache) return 'passthrough'
    return config.value.promptCacheMode === 'off' ? 'off' : 'stable'
  },
  set: (value: string) => {
    config.value.passthroughPromptCache = value === 'passthrough'
    config.value.promptCacheMode = value === 'off' ? 'off' : ''
  },
})
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
        <div><strong>透传请求体</strong><span>默认关闭，仅转发已支持字段；下方字段开关仍优先执行</span></div>
        <el-switch v-model="config.passthroughRequestBody" />
      </div>
      <div class="setting-row prompt-cache-row">
        <div><strong>提示缓存字段</strong><span>系统生成＝按用户、模型和渠道凭据生成稳定缓存键；不下发＝剥离客户端字段且不发送缓存键，上游不认该字段时必须选它；跟随客户端＝原样转发 prompt_cache_* 与缓存断点</span></div>
        <el-select v-model="promptCacheMode">
          <el-option label="系统生成稳定键" value="stable" />
          <el-option label="不下发缓存字段" value="off" />
          <el-option label="跟随客户端" value="passthrough" />
        </el-select>
      </div>
    </div>

    <div class="limit-field">
      <div class="section-caption">转发并发限制</div>
      <div class="setting-row">
        <div><strong>每把密钥的并发上限</strong><span>同一时刻允许进行的转发请求数，0 表示不限制；额度按「渠道 × 密钥」独立计数，本渠道配 3 把密钥即 3 倍并发</span></div>
        <el-input-number v-model="config.concurrencyLimit" :min="0" :max="1024" :step="1" controls-position="right" />
      </div>
    </div>

    <div class="conversion-field">
      <div class="section-caption">协议转换</div>
      <div class="setting-row">
        <div><strong>Chat / Responses 自动转换</strong><span>开启后 gpt-* 模型转投上游 /responses，上游不支持时自动回退；关闭后本渠道一律直连客户端声明的端点，用于排查转换引入的问题</span></div>
        <el-select v-model="protocolConversion">
          <el-option label="跟随系统" value="inherit" />
          <el-option label="启用" value="on" />
          <el-option label="关闭" value="off" />
        </el-select>
      </div>
    </div>

    <div class="proxy-field">
      <div class="field-label">
        <strong>代理地址</strong>
        <el-button size="small" :loading="testingProxy" :disabled="!(proxyUrl && proxyUrl.trim())" @click="emit('testProxy')">测试代理</el-button>
      </div>
      <el-input v-model="proxyUrl" type="textarea" :rows="3" clearable placeholder="每行一个代理地址，例如 http://user:pass@host:port" autocomplete="off" spellcheck="false" />
      <span>支持多行，每行一个代理（HTTP/HTTPS / SOCKS5），按行序与渠道密钥取模固定配对（2 个代理、5 把密钥即 1-1、2-2、3-1、4-2、5-1）；某代理失败时仅对应密钥顺延到下一个，其它密钥不变。留空表示不使用代理，保存时清空即删除</span>
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
.advanced-settings { margin-top: 20px; border-top: 1px solid #dce2e7; }.advanced-heading { display: flex; align-items: center; gap: 8px; padding: 16px 0 10px; color: #15202b; }.advanced-heading svg { color: #1677ff; }.advanced-heading strong, .setting-row strong, .proxy-field strong, .prompt-field > strong { font-size: 13px; }.setting-group, .field-controls { border-top: 1px solid #dce2e7; }.setting-row { display: flex; min-height: 61px; align-items: center; justify-content: space-between; gap: 16px; border-bottom: 1px solid #dce2e7; padding: 8px 0; }.setting-row > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.setting-row span, .proxy-field > span { color: #66717d; font-size: 11px; line-height: 1.45; }.setting-row :deep(.el-switch) { flex: 0 0 auto; }.prompt-cache-row :deep(.el-select) { flex: 0 0 auto; width: 156px; }.proxy-field, .prompt-field { display: flex; flex-direction: column; gap: 8px; padding: 16px 0; border-bottom: 1px solid #dce2e7; }.field-label { display: flex; align-items: center; justify-content: space-between; gap: 12px; }.field-label :deep(.el-button) { height: auto; padding: 0; }.compact { min-height: 52px; margin-top: 4px; border-bottom: 0; }.section-caption { padding: 14px 0 5px; color: #40505f; font-size: 12px; font-weight: 600; }.limit-field, .conversion-field { border-top: 1px solid #dce2e7; }.limit-field .setting-row :deep(.el-input-number), .conversion-field .setting-row :deep(.el-select) { flex: 0 0 auto; width: 132px; }@media (max-width: 480px) { .setting-row { align-items: flex-start; padding: 12px 0; }.setting-row :deep(.el-switch) { margin-top: 4px; } }
</style>
