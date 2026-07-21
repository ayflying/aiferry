<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { AlertCircle, LogIn, ShieldCheck } from '@lucide/vue'
import { loadAuthConfig } from '../api/auth'
import type { AuthConfig } from '../api/types'
import { authErrorMessage, localReturnTo } from '../lib/auth'
import RichContent from '../components/RichContent.vue'
import SiteFooter from '../components/SiteFooter.vue'
import { isHTTPURL } from '../lib/site-content'
import { useSystemStore } from '../stores/system'

const route = useRoute()
const config = ref<AuthConfig>()
const loading = ref(true)
const starting = ref(false)
const loadError = ref('')
const loginError = computed(() => authErrorMessage(route.query.error))
const system = useSystemStore()
const accepted = ref(false)
const agreementRequired = computed(() => Boolean(system.information.userAgreement.trim() || system.information.privacyPolicy.trim()))

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    config.value = await loadAuthConfig()
    system.apply(config.value.system)
  } catch {
    loadError.value = '认证服务暂时不可用，请稍后重试'
  } finally {
    loading.value = false
  }
}

function beginLogin() {
  if (!config.value?.enabled || starting.value || (agreementRequired.value && !accepted.value)) return
  starting.value = true
  const returnTo = localReturnTo(route.query.returnTo)
  window.location.assign(`${config.value.loginPath}?returnTo=${encodeURIComponent(returnTo)}`)
}

function documentTarget(value: string, path: string) {
  return isHTTPURL(value) ? value : path
}

function isExternal(value: string) {
  return isHTTPURL(value)
}

function resetLogo(event: Event) {
  ;(event.target as HTMLImageElement).src = '/aiferry-logo.png'
}

onMounted(load)
</script>

<template>
  <main class="login-page">
    <div class="login-masthead">
      <span class="brand-mark" role="img" :aria-label="system.systemName"><img class="brand-logo" :src="system.logoUrl" alt="" @error="resetLogo" /></span>
      <div class="brand-copy"><strong>{{ system.systemName }}</strong><span>AI 网关</span></div>
    </div>

    <div class="login-center">
      <section class="login-tool" aria-labelledby="login-title">
        <div class="login-route" aria-hidden="true">
          <span class="route-port casdoor-port"><ShieldCheck :size="17" /></span>
          <span class="route-line"><i /></span>
          <span class="route-port ferry-port"><img class="route-logo" :src="system.logoUrl" alt="" @error="resetLogo" /></span>
        </div>
        <div class="login-heading">
          <span class="mono-label">CONTROL DECK / SSO</span>
          <h1 id="login-title">登录 {{ system.systemName }}</h1>
          <p>{{ config?.provider || 'Casdoor' }} 统一身份认证</p>
        </div>

        <div v-if="loginError || loadError" class="auth-alert" role="alert">
          <AlertCircle :size="18" />
          <span>{{ loginError || loadError }}</span>
        </div>

        <el-checkbox v-if="agreementRequired" v-model="accepted" class="login-agreement">我已阅读并同意<a v-if="system.information.userAgreement.trim()" :href="documentTarget(system.information.userAgreement, '/terms')" :target="isExternal(system.information.userAgreement) ? '_blank' : undefined" :rel="isExternal(system.information.userAgreement) ? 'noopener noreferrer' : undefined" @click.stop>用户协议</a><span v-if="system.information.userAgreement.trim() && system.information.privacyPolicy.trim()">和</span><a v-if="system.information.privacyPolicy.trim()" :href="documentTarget(system.information.privacyPolicy, '/privacy')" :target="isExternal(system.information.privacyPolicy) ? '_blank' : undefined" :rel="isExternal(system.information.privacyPolicy) ? 'noopener noreferrer' : undefined" @click.stop>隐私政策</a></el-checkbox>

        <button class="login-command" type="button" :disabled="loading || !config?.enabled || starting || (agreementRequired && !accepted)" @click="beginLogin">
          <LogIn :size="19" />
          <span>{{ starting ? '正在前往 Casdoor' : loading ? '正在读取登录配置' : '使用 Casdoor 登录' }}</span>
        </button>
      </section>
      <section v-if="system.information.homeContent.trim()" class="login-home-content"><RichContent :value="system.information.homeContent" /></section>
    </div>

    <SiteFooter class="login-site-footer" />
  </main>
</template>
