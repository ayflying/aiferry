<script setup lang="ts">
import { useMediaQuery } from '../composables/useMediaQuery'

const props = withDefaults(defineProps<{ breakpoint?: number }>(), { breakpoint: 720 })

const isMobile = useMediaQuery(() => `(max-width: ${props.breakpoint}px)`)
</script>

<template>
  <slot v-if="isMobile" name="mobile" />
  <slot v-else name="desktop" />
</template>
