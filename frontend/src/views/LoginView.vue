<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { AlertCircle, LogIn, ShieldCheck, ShipWheel } from '@lucide/vue'
import { loadAuthConfig } from '../api/auth'
import type { AuthConfig } from '../api/types'
import { authErrorMessage, localReturnTo } from '../lib/auth'

const route = useRoute()
const config = ref<AuthConfig>()
const loading = ref(true)
const starting = ref(false)
const loadError = ref('')
const loginError = computed(() => authErrorMessage(route.query.error))

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    config.value = await loadAuthConfig()
  } catch {
    loadError.value = '认证服务暂时不可用，请稍后重试'
  } finally {
    loading.value = false
  }
}

function beginLogin() {
  if (!config.value?.enabled || starting.value) return
  starting.value = true
  const returnTo = localReturnTo(route.query.returnTo)
  window.location.assign(`${config.value.loginPath}?returnTo=${encodeURIComponent(returnTo)}`)
}

onMounted(load)
</script>

<template>
  <main class="login-page">
    <div class="login-masthead">
      <span class="brand-mark"><ShipWheel :size="22" /></span>
      <div class="brand-copy"><strong>AiFerry</strong><span>AI 摆渡</span></div>
    </div>

    <section class="login-tool" aria-labelledby="login-title">
      <div class="login-route" aria-hidden="true">
        <span class="route-port casdoor-port"><ShieldCheck :size="17" /></span>
        <span class="route-line"><i /></span>
        <span class="route-port ferry-port"><ShipWheel :size="17" /></span>
      </div>
      <div class="login-heading">
        <span class="mono-label">CONTROL DECK / SSO</span>
        <h1 id="login-title">登录 AiFerry</h1>
        <p>{{ config?.provider || 'Casdoor' }} 统一身份认证</p>
      </div>

      <div v-if="loginError || loadError" class="auth-alert" role="alert">
        <AlertCircle :size="18" />
        <span>{{ loginError || loadError }}</span>
      </div>

      <button class="login-command" type="button" :disabled="loading || !config?.enabled || starting" @click="beginLogin">
        <LogIn :size="19" />
        <span>{{ starting ? '正在前往 Casdoor' : loading ? '正在读取登录配置' : '使用 Casdoor 登录' }}</span>
      </button>
    </section>

    <div class="login-foot mono-label">AIFERRY · OPENAI GATEWAY</div>
  </main>
</template>
