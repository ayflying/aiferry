<script setup lang="ts">
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
}>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; restart: [] }>()

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
</script>

<template>
  <el-dialog :model-value="modelValue" title="登录上游账号" width="min(560px, 94vw)" append-to-body @update:model-value="emit('update:modelValue', $event)">
    <el-alert type="info" :closable="false" show-icon :title="session?.userCode ? '复制下方用户码，在平台官方登录页输入用户码并完成设备授权；凭据会自动保存到当前渠道。' : '在浏览器里完成平台官方登录，本页会自动等待并接收凭据；登录地址是一次性的，请尽快完成。'" />
    <div v-if="session?.userCode" class="login-row">
      <el-input :model-value="session.userCode" readonly aria-label="设备授权用户码" class="user-code" />
      <el-button :icon="Copy" @click="copyCode">复制用户码</el-button>
    </div>
    <div class="login-row">
      <el-input :model-value="session?.authUrl || ''" readonly aria-label="平台登录地址" />
      <el-button type="primary" :disabled="!session?.authUrl || Boolean(error)" @click="openLoginUrl">打开登录页</el-button>
    </div>
    <div class="login-status" role="status">
      <LoaderCircle v-if="!error" :size="15" class="spinner" />
      <span>{{ error || '等待你在浏览器完成登录…' }}</span>
      <small v-if="!error">已等待 {{ elapsed }} 秒 · 剩余 {{ Math.max(0, Math.ceil(expiresIn - elapsed)) }} 秒</small>
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">关闭</el-button>
      <el-button type="primary" :loading="starting" @click="emit('restart')">重新发起登录</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.login-row { display: flex; gap: 8px; margin-top: 12px; }
.user-code { font-family: 'JetBrains Mono', monospace; }
.login-status { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 12px; color: #5b6a77; font-size: 12px; }
.spinner { animation: login-spin 1s linear infinite; }
@keyframes login-spin { to { transform: rotate(360deg); } }
</style>
