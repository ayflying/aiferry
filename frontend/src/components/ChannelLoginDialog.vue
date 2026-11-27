<script setup lang="ts">
import { computed, ref } from 'vue'
import { Copy, LoaderCircle } from '@lucide/vue'
import type { ChannelLoginSession } from '../api/types'
import { copyText } from '../lib/clipboard'
import { showError, showSuccess } from '../lib/error'

const props = defineProps<{
  modelValue: boolean
  session: ChannelLoginSession | null
  error: string
  elapsed: number
  expiresIn: number
  starting: boolean
  completing?: boolean
  /** 回调落在管理员本机时（Antigravity 桥接），需要把回调地址粘回来提交。 */
  callbackRequired?: boolean
  reloginAccount?: string
}>()
const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  restart: []
  complete: [callbackUrl: string]
}>()

const callbackUrl = ref('')

const alertTitle = computed(() => {
  if (props.callbackRequired) return '在浏览器打开下方链接完成 Google 授权，再把浏览器地址栏里的回调地址粘贴到下方提交。'
  if (props.session?.userCode) return '复制下方用户码，在平台官方登录页输入用户码并完成设备授权。'
  return '请在浏览器打开平台官方登录页并完成授权。登录地址是一次性的，请尽快完成。'
})

const statusText = computed(() => {
  if (props.error) return props.error
  return props.callbackRequired ? '授权完成后，把回调地址粘贴到上面提交…' : '等待你在浏览器完成登录…'
})

async function copyCode() {
  if (!props.session?.userCode) return
  try {
    await copyText(props.session.userCode)
    showSuccess('用户码已复制')
  } catch (error) { showError(error, '复制用户码失败') }
}
function openLoginUrl() {
  if (props.session?.authUrl) window.open(props.session.authUrl, '_blank', 'noopener')
}
function submitCallback() {
  const value = callbackUrl.value.trim()
  if (!value) return
  emit('complete', value)
}
</script>

<template>
  <el-dialog :model-value="modelValue" :title="reloginAccount ? `重新登录 · ${reloginAccount}` : '登录上游账号'" width="min(560px, 94vw)" append-to-body @update:model-value="emit('update:modelValue', $event)">
    <el-alert v-if="!session && reloginAccount" type="info" :closable="false" show-icon title="请使用原账号登录。授权后将更新此账号的凭据，不会添加新账号。" />
    <template v-else>
      <el-alert type="info" :closable="false" show-icon :title="alertTitle" />
      <div v-if="session?.userCode" class="login-row">
        <el-input :model-value="session.userCode" readonly aria-label="设备授权用户码" class="user-code" />
        <el-button :icon="Copy" @click="copyCode">复制用户码</el-button>
      </div>
      <div class="login-row">
        <el-input :model-value="session?.authUrl || ''" readonly aria-label="平台登录地址" />
        <el-button type="primary" :disabled="!session?.authUrl || Boolean(error)" @click="openLoginUrl">打开登录页</el-button>
      </div>
      <div v-if="callbackRequired && session" class="login-callback">
        <p class="callback-hint">
          授权后浏览器会跳到 <code>http://localhost:51121/oauth-callback?code=…</code> 并提示「无法访问」，这是正常的：
          把地址栏里的完整地址粘贴到下面提交即可，也可以只粘贴 <code>code</code> 参数。
        </p>
        <div class="login-row">
          <el-input v-model="callbackUrl" aria-label="授权回调地址" placeholder="粘贴授权后的回调地址或 code" @keyup.enter="submitCallback" />
          <el-button type="primary" :loading="completing" :disabled="!callbackUrl.trim()" @click="submitCallback">完成登录</el-button>
        </div>
      </div>
      <div class="login-status" role="status">
        <LoaderCircle v-if="!error && !callbackRequired" :size="15" class="spinner" />
        <span>{{ statusText }}</span>
        <small v-if="!error">已等待 {{ elapsed }} 秒 · 剩余 {{ Math.max(0, Math.ceil(expiresIn - elapsed)) }} 秒</small>
      </div>
    </template>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">关闭</el-button>
      <el-button type="primary" :loading="starting" @click="emit('restart')">{{ session || error ? '重新发起登录' : reloginAccount ? '开始重新登录' : '发起登录' }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.login-row { display: flex; gap: 8px; margin-top: 12px; }
.user-code { font-family: 'JetBrains Mono', monospace; }
.login-callback { margin-top: 4px; }
.callback-hint { margin: 12px 0 0; color: #5b6a77; font-size: 12px; line-height: 1.6; }
.callback-hint code { padding: 1px 4px; border-radius: 4px; background: #f2f4f7; font-family: 'JetBrains Mono', monospace; }
.login-status { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 12px; color: #5b6a77; font-size: 12px; }
.spinner { animation: login-spin 1s linear infinite; }
@keyframes login-spin { to { transform: rotate(360deg); } }
</style>
