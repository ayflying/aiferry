<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{ breakpoint?: number }>(), { breakpoint: 720 })

const isMobile = ref(false)
let query: MediaQueryList | undefined

function updateLayout() {
  isMobile.value = query?.matches ?? false
}

onMounted(() => {
  query = window.matchMedia(`(max-width: ${props.breakpoint}px)`)
  updateLayout()
  query.addEventListener('change', updateLayout)
})

onBeforeUnmount(() => query?.removeEventListener('change', updateLayout))
</script>

<template>
  <slot v-if="isMobile" name="mobile" />
  <slot v-else name="desktop" />
</template>
