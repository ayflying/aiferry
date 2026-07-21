<script setup lang="ts">
import { computed } from 'vue'
import { isHTTPURL } from '../lib/site-content'
import { useSystemStore } from '../stores/system'

const site = useSystemStore()
const footerText = computed(() => site.information.footer || `${site.systemName} · OPENAI GATEWAY`)
const termsURL = computed(() => site.information.userAgreement.trim())
const privacyURL = computed(() => site.information.privacyPolicy.trim())

function documentTarget(value: string, path: string) {
  return isHTTPURL(value) ? value : path
}

function isExternal(value: string) {
  return isHTTPURL(value)
}
</script>

<template>
  <footer class="site-footer">
    <span>{{ footerText }}</span>
    <nav class="footer-links" aria-label="站点信息">
      <a v-if="site.information.about.trim()" href="/about">关于</a>
      <a v-if="termsURL" :href="documentTarget(termsURL, '/terms')" :target="isExternal(termsURL) ? '_blank' : undefined" :rel="isExternal(termsURL) ? 'noopener noreferrer' : undefined">用户协议</a>
      <a v-if="privacyURL" :href="documentTarget(privacyURL, '/privacy')" :target="isExternal(privacyURL) ? '_blank' : undefined" :rel="isExternal(privacyURL) ? 'noopener noreferrer' : undefined">隐私政策</a>
    </nav>
  </footer>
</template>

<style scoped>
.site-footer { display: flex; min-height: 52px; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 24px; border-top: 1px solid #dce2e7; color: #66717d; font-size: 11px; }.footer-links { display: flex; flex-wrap: wrap; gap: 14px; }.footer-links a { color: inherit; text-decoration: none; }.footer-links a:hover { color: #1677ff; }@media (max-width: 600px) { .site-footer { align-items: flex-start; flex-direction: column; padding-inline: 14px; gap: 8px; } }
</style>
