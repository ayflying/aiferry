<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import RichContent from '../components/RichContent.vue'
import SiteFooter from '../components/SiteFooter.vue'
import { isHTTPURL } from '../lib/site-content'
import { useSystemStore } from '../stores/system'

const route = useRoute()
const site = useSystemStore()
const kind = computed(() => String(route.meta.document || 'about'))
const documentInfo = computed(() => {
  if (kind.value === 'terms') return { title: '用户协议', value: site.information.userAgreement, redirect: true }
  if (kind.value === 'privacy') return { title: '隐私政策', value: site.information.privacyPolicy, redirect: true }
  return { title: '关于', value: site.information.about, redirect: false }
})
const externalURL = computed(() => documentInfo.value.value.trim())
const isExternal = computed(() => isHTTPURL(externalURL.value))

function redirectExternal() {
  if (documentInfo.value.redirect && isExternal.value) window.location.replace(externalURL.value)
}

function resetLogo(event: Event) {
  ;(event.target as HTMLImageElement).src = '/aiferry-logo.png'
}

onMounted(async () => {
  try { await site.load() } finally { redirectExternal() }
})
watch(externalURL, redirectExternal)
</script>

<template>
  <main class="public-document">
    <header class="document-head"><a class="document-brand" href="/login"><img :src="site.logoUrl" alt="" @error="resetLogo" /><strong>{{ site.systemName }}</strong></a></header>
    <article class="document-body"><h1>{{ documentInfo.title }}</h1><iframe v-if="kind === 'about' && isExternal" :src="externalURL" title="关于" sandbox="" referrerpolicy="no-referrer" loading="lazy" /><RichContent v-else-if="documentInfo.value.trim()" :value="documentInfo.value" /><p v-else class="empty-document">暂未配置{{ documentInfo.title }}。</p></article>
    <SiteFooter />
  </main>
</template>

<style scoped>
.public-document { display: flex; min-height: 100vh; flex-direction: column; background: #f6f8fa; }.document-head { display: flex; min-height: 68px; align-items: center; padding: 0 max(24px, calc((100% - 1120px) / 2)); border-bottom: 1px solid #dce2e7; background: #fff; }.document-brand { display: inline-flex; align-items: center; gap: 10px; color: #15202b; text-decoration: none; }.document-brand img { width: 30px; height: 30px; object-fit: contain; }.document-brand strong { font-size: 17px; }.document-body { width: min(920px, calc(100% - 32px)); flex: 1; margin: 0 auto; padding: 44px 0 56px; }.document-body h1 { margin: 0 0 28px; color: #15202b; font-size: 26px; }.document-body iframe { width: 100%; min-height: 620px; border: 1px solid #dce2e7; border-radius: 5px; background: #fff; }.empty-document { color: #66717d; }@media (max-width: 600px) { .document-head { padding-inline: 16px; }.document-body { padding-top: 30px; } }
</style>
