<script setup lang="ts">
defineProps<{ name: string; code: string }>()
</script>

<template>
  <div class="name-code-cell">
    <strong>{{ name }}</strong>
    <code>{{ code }}</code>
  </div>
</template>

<style scoped>
.name-code-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.name-code-cell strong { font-size: 13px; }
.name-code-cell code { overflow: hidden; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
</style>
