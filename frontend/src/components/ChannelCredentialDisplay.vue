<script setup lang="ts">
import { computed } from 'vue'
import { channelCredentialReference } from '../lib/usage'

const props = defineProps<{
  channelName?: string
  credentialIndex?: number
  credentialRemark?: string
  apiKeyName?: string
}>()

// 备注是运维给密钥起的自定义名字，比「渠道 #序号」更好识别。
// 只有去除空白后非空才展示，避免空备注在列表里留下一行空白。
const remark = computed(() => props.credentialRemark?.trim() ?? '')
</script>

<template>
  <div class="channel-credential-display">
    <strong>{{ channelCredentialReference(channelName, credentialIndex) }}</strong>
    <span v-if="remark" class="credential-remark" :title="remark">{{ remark }}</span>
    <small>{{ apiKeyName?.trim() || '未记录访问密钥' }}</small>
  </div>
</template>

<style scoped>
.credential-remark {
  align-self: flex-start;
  max-width: 100%;
  overflow: hidden;
  padding: 0 6px;
  border-radius: 4px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-regular);
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
