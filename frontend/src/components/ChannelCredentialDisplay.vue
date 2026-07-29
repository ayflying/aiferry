<script setup lang="ts">
import { channelCredentialLabel } from '../lib/usage'

defineProps<{
  channelName?: string
  credentialIndex?: number
}>()
</script>

<template>
  <div class="channel-credential-display">
    <strong>{{ channelName?.trim() || '已删除渠道' }}</strong>
    <small>{{ channelCredentialLabel(credentialIndex) }}</small>
  </div>
</template>
