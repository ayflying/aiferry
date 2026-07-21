<script setup lang="ts">
withDefaults(defineProps<{ loading?: boolean }>(), { loading: false })
</script>

<template>
  <div class="mobile-record-list" :aria-busy="loading">
    <slot />
    <div v-if="loading" class="mobile-record-list__loading">正在加载...</div>
  </div>
</template>
