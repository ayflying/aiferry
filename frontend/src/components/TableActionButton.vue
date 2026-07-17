<script setup lang="ts">
import type { Component } from 'vue'

withDefaults(defineProps<{
  icon: Component
  label: string
  danger?: boolean
  disabled?: boolean
  size?: number
}>(), {
  danger: false,
  disabled: false,
  size: 16,
})

const emit = defineEmits<{ click: [] }>()
</script>

<template>
  <el-tooltip :content="label">
    <span class="action-button-wrap">
      <button class="icon-button" :class="{ danger }" type="button" :aria-label="label" :disabled="disabled" @click="emit('click')">
        <component :is="icon" :size="size" />
      </button>
    </span>
  </el-tooltip>
</template>

<style scoped>
.action-button-wrap { display: inline-flex; }
</style>
