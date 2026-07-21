<script setup lang="ts">
import { computed } from 'vue'
import { renderSiteContent } from '../lib/site-content'

const props = defineProps<{ value: string }>()
const html = computed(() => renderSiteContent(props.value))
</script>

<template>
  <!-- Content is sanitized by renderSiteContent before it reaches v-html. -->
  <div class="rich-content" v-html="html" />
</template>

<style scoped>
.rich-content { color: #33414e; font-size: 14px; line-height: 1.75; overflow-wrap: anywhere; }.rich-content :deep(:first-child) { margin-top: 0; }.rich-content :deep(:last-child) { margin-bottom: 0; }.rich-content :deep(h1), .rich-content :deep(h2), .rich-content :deep(h3) { color: #15202b; line-height: 1.3; }.rich-content :deep(h1) { font-size: 24px; }.rich-content :deep(h2) { font-size: 19px; }.rich-content :deep(h3) { font-size: 16px; }.rich-content :deep(a) { color: #1677ff; }.rich-content :deep(pre) { overflow: auto; padding: 14px; border: 1px solid #dce2e7; border-radius: 5px; background: #f7f9fa; }.rich-content :deep(code) { font-family: 'JetBrains Mono', monospace; font-size: .9em; }.rich-content :deep(iframe) { display: block; width: 100%; min-height: 480px; border: 1px solid #dce2e7; border-radius: 5px; background: #fff; }
</style>
