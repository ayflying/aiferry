<script setup lang="ts">
import { channelCredentialReference } from '../lib/usage'

defineProps<{
  channelName?: string
  credentialIndex?: number
  apiKeyName?: string
}>()
</script>

<template>
  <div class="channel-credential-display">
    <strong>{{ channelCredentialReference(channelName, credentialIndex) }}</strong>
    <small>{{ apiKeyName?.trim() ? `密钥 ${apiKeyName.trim()}` : '未记录访问密钥' }}</small>
  </div>
</template>
