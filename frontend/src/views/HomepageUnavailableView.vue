<script setup lang="ts">
import { CircleOff } from '@lucide/vue'
import SiteFooter from '../components/SiteFooter.vue'
import { useSystemStore } from '../stores/system'

const system = useSystemStore()

function resetLogo(event: Event) {
  ;(event.target as HTMLImageElement).src = '/aiferry-logo.png'
}
</script>

<template>
  <main class="unavailable-page">
    <header class="unavailable-header">
      <div class="unavailable-brand">
        <img :src="system.logoUrl" alt="" @error="resetLogo" />
        <strong>{{ system.systemName }}</strong>
      </div>
    </header>

    <section class="unavailable-content" aria-labelledby="unavailable-title">
      <CircleOff :size="30" aria-hidden="true" />
      <p>404</p>
      <h1 id="unavailable-title">页面不可用</h1>
    </section>

    <SiteFooter />
  </main>
</template>

<style scoped>
.unavailable-page { display: grid; min-height: 100vh; grid-template-rows: auto 1fr auto; color: #15202b; background: #f6f8fa; }
.unavailable-header { display: flex; min-height: 68px; align-items: center; padding: 0 max(24px, calc((100% - 1120px) / 2)); border-bottom: 1px solid #dce2e7; background: #fff; }
.unavailable-brand { display: inline-flex; align-items: center; gap: 10px; }.unavailable-brand img { width: 30px; height: 30px; object-fit: contain; }.unavailable-brand strong { font-size: 17px; }
.unavailable-content { display: grid; align-content: center; justify-items: center; gap: 10px; padding: 48px 24px; text-align: center; }.unavailable-content svg { color: #66717d; }.unavailable-content p { margin: 0; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.unavailable-content h1 { margin: 0; font-size: 22px; font-weight: 650; }
@media (max-width: 600px) { .unavailable-header { padding-inline: 16px; }.unavailable-content { padding-inline: 16px; } }
</style>
