<script setup lang="ts">
import { Copy, LoaderCircle } from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import type { ChannelLoginProject, ChannelLoginResult, ChannelLoginSession } from '../api/types'
import { copyText } from '../lib/clipboard'
import { showError, showSuccess } from '../lib/error'

const props = defineProps<{
  modelValue: boolean
  session: ChannelLoginSession | null
  error: string
  elapsed: number
  expiresIn: number
  starting: boolean
  googleAdd?: boolean
  projectStep: Extract<ChannelLoginResult, { status: 'project_required' }> | null
  confirmingProject: boolean
  manualRetryAvailable: boolean
  reloginAccount?: string
}>()
const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  restart: []
  'confirm-project': [projectId: string]
  'retry-with-project': [projectId: string]
}>()

const selectedProjectId = ref('')
const manualProjectId = ref('')
const manualMode = ref(false)
const hasProjects = computed(() => Boolean(props.projectStep?.projects?.length))
const useManualInput = computed(() => !hasProjects.value || manualMode.value)
const projectId = computed(() => ((useManualInput.value ? manualProjectId.value : selectedProjectId.value) ?? '').trim())
const showProjectForm = computed(() => props.projectStep !== null || props.manualRetryAvailable)
const projectBusy = computed(() => props.starting || props.confirmingProject)

watch(() => props.modelValue, (open) => { if (!open) resetProject() })

function resetProject() {
  selectedProjectId.value = ''
  manualProjectId.value = ''
  manualMode.value = false
}
function beginLogin() {
  if (projectBusy.value) return
  resetProject()
  emit('restart')
}
function submitProject() {
  if (projectBusy.value || !projectId.value) return
  if (props.projectStep) emit('confirm-project', projectId.value)
  else if (props.manualRetryAvailable) emit('retry-with-project', projectId.value)
}
function projectLabel(project: ChannelLoginProject) {
  return project.displayName && project.displayName !== project.projectId ? `${project.displayName} (${project.projectId})` : project.projectId
}

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
  <el-dialog :model-value="modelValue" :title="reloginAccount ? `重新登录 · ${reloginAccount}` : googleAdd ? '添加 Google 账号' : '登录上游账号'" width="min(560px, 94vw)" append-to-body @update:model-value="emit('update:modelValue', $event)">
    <template v-if="showProjectForm">
      <el-alert
        type="info" :closable="false" show-icon
        :title="projectStep ? 'Google 授权已完成，请确认此账号要使用的 Google Cloud 项目。' : '可以手动填写项目 ID，再次授权时不请求读取项目列表。'"
        :description="projectStep?.projectMessage"
      />
      <el-form label-position="top" class="project-form" @submit.prevent="submitProject">
        <el-form-item label="Google Cloud 项目 ID" required>
          <template #label>
            <span class="project-label">
              Google Cloud 项目 ID
              <el-link v-if="useManualInput" href="https://console.cloud.google.com/cloud-resource-manager" target="_blank" rel="noopener noreferrer" type="primary">查看项目 ID</el-link>
            </span>
          </template>
          <el-input v-if="useManualInput" v-model="manualProjectId" :disabled="projectBusy" clearable placeholder="例如 my-cloud-project" aria-describedby="gemini-project-id-help" />
          <el-select v-else v-model="selectedProjectId" filterable clearable :disabled="projectBusy" placeholder="按项目名称或 ID 搜索" class="project-select">
            <el-option v-for="project in projectStep?.projects" :key="project.projectId" :value="project.projectId" :label="projectLabel(project)" />
          </el-select>
          <small v-if="useManualInput" id="gemini-project-id-help" class="project-help">用授权所用的 Google 账号打开项目列表，复制「项目 ID」，不要填项目名称或项目编号。</small>
          <el-button v-if="hasProjects" link type="primary" :disabled="projectBusy" @click="manualMode = !manualMode">{{ manualMode ? '从项目列表选择' : '手动输入项目 ID' }}</el-button>
        </el-form-item>
      </el-form>
      <el-alert v-if="error" type="error" :closable="false" show-icon :title="error" />
    </template>
    <el-alert v-else-if="!session && reloginAccount" type="info" :closable="false" show-icon title="请使用原账号登录。授权后将更新此账号的凭据，不会添加新账号。" />
    <template v-else>
      <el-alert type="info" :closable="false" show-icon :title="session?.userCode ? '复制下方用户码，在平台官方登录页输入用户码并完成设备授权。' : googleAdd ? '请在 Google 登录页完成授权，再返回此窗口。' : '请在浏览器打开平台官方登录页并完成授权。登录地址是一次性的，请尽快完成。'" />
      <div v-if="session?.userCode" class="login-row">
        <el-input :model-value="session.userCode" readonly aria-label="设备授权用户码" class="user-code" />
        <el-button :icon="Copy" @click="copyCode">复制用户码</el-button>
      </div>
      <div class="login-row">
        <el-input :model-value="session?.authUrl || ''" readonly aria-label="平台登录地址" />
        <el-button type="primary" :disabled="!session?.authUrl || Boolean(error)" @click="openLoginUrl">{{ googleAdd ? '打开 Google 登录页' : '打开登录页' }}</el-button>
      </div>
      <div class="login-status" role="status">
        <LoaderCircle v-if="!error" :size="15" class="spinner" />
        <span>{{ error || (starting && googleAdd ? '正在获取 Google 登录地址…' : '等待你在浏览器完成登录…') }}</span>
        <small v-if="!error && (session || !googleAdd)">已等待 {{ elapsed }} 秒 · 剩余 {{ Math.max(0, Math.ceil(expiresIn - elapsed)) }} 秒</small>
      </div>
    </template>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">关闭</el-button>
      <el-button v-if="!showProjectForm || error" :type="showProjectForm ? 'default' : 'primary'" :loading="starting" :disabled="confirmingProject" @click="beginLogin">{{ session || !reloginAccount ? '重新发起登录' : '开始重新登录' }}</el-button>
      <el-button v-if="showProjectForm" type="primary" :loading="projectBusy" :disabled="!projectId" @click="submitProject">{{ projectStep ? '确认项目' : '使用此项目登录' }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.project-form { margin-top: 12px; }
.project-label { display: inline-flex; flex-wrap: wrap; align-items: baseline; gap: 0 8px; }
.project-help { display: block; width: 100%; margin-top: 4px; color: var(--el-text-color-secondary); line-height: 1.5; }
.project-select { width: 100%; }
.login-row { display: flex; gap: 8px; margin-top: 12px; }
.user-code { font-family: 'JetBrains Mono', monospace; }
.login-status { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 12px; color: #5b6a77; font-size: 12px; }
.spinner { animation: login-spin 1s linear infinite; }
@keyframes login-spin { to { transform: rotate(360deg); } }
</style>
