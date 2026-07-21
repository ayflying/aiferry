<script setup lang="ts">
import { computed } from 'vue'
import { Anchor, Radio, ShipWheel } from '@lucide/vue'
import type { Channel } from '../api/types'
import { displayedRoutes, isChannelRoutable, isChannelRouteAlert } from '../lib/route-display'
import { useSystemStore } from '../stores/system'

const props = defineProps<{ channels: Channel[] }>()
const visible = computed(() => displayedRoutes(props.channels))
const system = useSystemStore()
</script>

<template>
  <div class="route-strip">
    <div class="route-origin">
      <span class="route-icon"><ShipWheel :size="20" /></span>
      <div><strong>{{ system.systemName }}</strong><small>中转入口</small></div>
    </div>
    <div class="route-track" :class="{ active: visible.some((item) => isChannelRoutable(item)) }">
      <span class="route-pulse"><Radio :size="13" /></span>
    </div>
    <div v-if="visible.length" class="route-destinations">
      <div v-for="channel in visible" :key="channel.id" class="route-node" :class="{ online: isChannelRoutable(channel), failed: isChannelRouteAlert(channel), unavailable: !isChannelRoutable(channel) }">
        <Anchor :size="14" />
        <span>{{ channel.name }}</span>
        <small>P{{ channel.priority }}</small>
      </div>
    </div>
    <div v-else class="route-empty">暂无航线</div>
  </div>
</template>

<style scoped>
.route-strip { display: grid; min-height: 112px; grid-template-columns: auto minmax(80px, 1fr) minmax(280px, 2fr); align-items: center; gap: 16px; padding: 18px; overflow: hidden; border: 1px solid #cfd8df; border-radius: 6px; background: #fff; }
.route-origin { display: flex; align-items: center; gap: 10px; }
.route-origin > div { display: flex; flex-direction: column; }
.route-origin strong { font-size: 14px; }
.route-origin small { color: #66717d; font-size: 11px; }
.route-icon { display: grid; width: 38px; height: 38px; place-items: center; border-radius: 6px; color: #fff; background: #15202b; }
.route-track { position: relative; height: 2px; background: #dce2e7; }
.route-track::before, .route-track::after { position: absolute; top: -3px; width: 8px; height: 8px; border-radius: 50%; background: #a8b2bc; content: ''; }
.route-track::before { left: 0; }.route-track::after { right: 0; }
.route-track.active { background: #8ccabb; }
.route-track.active::before, .route-track.active::after { background: #16866f; }
.route-pulse { position: absolute; top: -12px; left: 45%; display: grid; width: 26px; height: 26px; place-items: center; border: 1px solid #8ccabb; border-radius: 50%; color: #16866f; background: #e5f5f1; }
.route-track.active .route-pulse { animation: transit 2.4s ease-in-out infinite; }
.route-destinations { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 7px; }
.route-node { display: grid; min-width: 0; grid-template-columns: auto 1fr auto; align-items: center; gap: 6px; padding: 8px 9px; border: 1px solid #dce2e7; border-radius: 5px; color: #66717d; background: #f8fafb; }
.route-node span { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.route-node small { font-family: 'JetBrains Mono', monospace; font-size: 10px; }
.route-node.online { border-color: #acd7cc; color: #126c5b; background: #f2faf8; }
.route-node.failed { border-color: #e9abb2; color: #a62f3d; background: #fff6f7; }
.route-node.unavailable { border-color: #e9abb2; color: #a62f3d; background: #fff6f7; }
.route-empty { color: #7b8792; font-size: 12px; text-align: center; }
@keyframes transit { 0%, 100% { left: 8%; } 50% { left: calc(92% - 26px); } }
@media (max-width: 780px) { .route-strip { grid-template-columns: 1fr; }.route-track { width: 100%; }.route-destinations { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (prefers-reduced-motion: reduce) { .route-track.active .route-pulse { animation: none; } }
</style>
