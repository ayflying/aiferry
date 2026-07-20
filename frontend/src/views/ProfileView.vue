<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, CircleDollarSign, RefreshCw, Save, Ticket, UserRound } from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { loadPersonalUsage, loadProfile, updateProfile } from '../api/auth'
import { apiPost } from '../api/client'
import type { AccountProfile, AccountUsageSummary, RedemptionResult } from '../api/types'
import { showError } from '../lib/error'
import { formatCost, formatNumber } from '../lib/format'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const loading = ref(false)
const saving = ref(false)
const redeeming = ref(false)
const redemptionCode = ref('')
const profile = ref<AccountProfile>()
const usage = ref<AccountUsageSummary>({ days: 30, requests: 0, successes: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, estimatedCost: 0 })
const form = reactive({ nickname: '', email: '' })
const successRate = computed(() => usage.value.requests ? `${((usage.value.successes / usage.value.requests) * 100).toFixed(1)}%` : '—')

async function load() {
  loading.value = true
  try {
    const [account, summary] = await Promise.all([loadProfile(), loadPersonalUsage()])
    profile.value = account
    usage.value = summary
    Object.assign(form, { nickname: account.nickname, email: account.email })
  } catch (error) { showError(error, '加载个人中心失败') } finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    profile.value = await updateProfile({ nickname: form.nickname.trim(), email: form.email.trim() })
    Object.assign(form, { nickname: profile.value.nickname, email: profile.value.email })
    await auth.ensureUser(true)
    ElMessage.success('个人资料已保存')
  } catch (error) { showError(error, '保存个人资料失败') } finally { saving.value = false }
}

async function redeem() {
  const code = redemptionCode.value.trim().toUpperCase()
  if (!code) { showError('请填写兑换码', '无法兑换'); return }
  redeeming.value = true
  try {
    const result = await apiPost<RedemptionResult>('/redemption-codes/redeem', { code })
    const account = await loadProfile()
    profile.value = account
    Object.assign(form, { nickname: account.nickname, email: account.email })
    redemptionCode.value = ''
    await auth.ensureUser(true)
    ElMessage.success(`兑换成功，已到账 ${formatCost(result.amount)}`)
  } catch (error) { showError(error, '兑换失败') } finally { redeeming.value = false }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page-stack profile-page">
    <div class="page-toolbar">
      <div class="profile-identity"><el-avatar :size="46" :src="profile?.avatarUrl || undefined"><UserRound :size="20" /></el-avatar><div><strong>{{ profile?.nickname || '用户' }}</strong><span>{{ profile?.email || '尚未绑定邮箱' }}</span></div></div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <section class="metric-grid" aria-label="个人账户摘要">
      <article class="metric-card"><div class="label"><CircleDollarSign :size="15" />账户余额</div><div class="value">{{ formatCost(profile?.balance) }}</div><div class="detail">USD</div></article>
      <article class="metric-card"><div class="label"><Activity :size="15" />近 {{ usage.days }} 天调用</div><div class="value">{{ formatNumber(usage.requests) }}</div><div class="detail">成功率 {{ successRate }}</div></article>
      <article class="metric-card"><div class="label"><Activity :size="15" />Token 使用</div><div class="value">{{ formatNumber(usage.totalTokens) }}</div><div class="detail">输入 {{ formatNumber(usage.inputTokens) }} · 输出 {{ formatNumber(usage.outputTokens) }}</div></article>
      <article class="metric-card"><div class="label"><CircleDollarSign :size="15" />估算费用</div><div class="value">{{ formatCost(usage.estimatedCost) }}</div><div class="detail">近 {{ usage.days }} 天</div></article>
    </section>

    <section class="profile-section">
      <div class="section-heading"><div><h2>个人资料</h2><span>昵称仅在 AiFerry 内显示，邮箱用于账户联系。</span></div></div>
      <el-form label-position="top" class="profile-form">
        <div class="form-grid"><el-form-item label="昵称"><el-input v-model="form.nickname" maxlength="64" show-word-limit /></el-form-item><el-form-item label="邮箱"><el-input v-model="form.email" clearable placeholder="name@example.com" /></el-form-item></div>
        <el-button type="primary" :icon="Save" :loading="saving" @click="save">保存资料</el-button>
      </el-form>
    </section>

    <section class="profile-section redemption-section">
      <div class="section-heading"><div><h2>兑换码</h2><span>兑换成功后，额度会立即计入账户余额。</span></div></div>
      <div class="redemption-box">
        <span class="redemption-icon"><Ticket :size="18" /></span>
        <el-input v-model="redemptionCode" maxlength="64" clearable placeholder="输入兑换码" @keyup.enter="redeem" />
        <el-button type="primary" :loading="redeeming" :disabled="!redemptionCode.trim()" @click="redeem">立即兑换</el-button>
      </div>
    </section>

    <section class="profile-section usage-section">
      <div class="section-heading"><div><h2>个人用量</h2><span>仅统计当前账户最近 {{ usage.days }} 天的中转调用。</span></div></div>
      <div class="usage-breakdown"><div><span>成功请求</span><strong>{{ formatNumber(usage.successes) }}</strong></div><div><span>输入 Token</span><strong>{{ formatNumber(usage.inputTokens) }}</strong></div><div><span>输出 Token</span><strong>{{ formatNumber(usage.outputTokens) }}</strong></div><div><span>估算费用</span><strong>{{ formatCost(usage.estimatedCost) }}</strong></div></div>
    </section>
  </div>
</template>

<style scoped>
.profile-identity { display: flex; min-width: 0; align-items: center; gap: 12px; }.profile-identity div { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.profile-identity strong { color: #15202b; font-size: 15px; }.profile-identity span, .profile-section .section-heading span { color: #66717d; font-size: 12px; }.profile-section { padding: 4px 0 22px; border-top: 1px solid #dce2e7; }.profile-form { max-width: 720px; margin-top: 18px; }.redemption-section { padding-bottom: 22px; }.redemption-box { display: flex; width: min(100%, 720px); align-items: center; gap: 10px; margin-top: 18px; }.redemption-box .el-input { flex: 1; }.redemption-icon { display: grid; width: 34px; height: 34px; flex: 0 0 34px; place-items: center; border: 1px solid #acd7cc; border-radius: 5px; color: #16866f; background: #e5f5f1; }.usage-section { padding-bottom: 0; }.usage-breakdown { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin-top: 18px; border: 1px solid #dce2e7; }.usage-breakdown div { display: flex; min-height: 72px; flex-direction: column; justify-content: center; gap: 6px; padding: 0 14px; border-right: 1px solid #dce2e7; }.usage-breakdown div:last-child { border-right: 0; }.usage-breakdown span { color: #66717d; font-size: 11px; }.usage-breakdown strong { color: #15202b; font-family: 'JetBrains Mono', monospace; font-size: 13px; }@media (max-width: 720px) { .usage-breakdown { grid-template-columns: repeat(2, minmax(0, 1fr)); }.usage-breakdown div:nth-child(2) { border-right: 0; }.usage-breakdown div:nth-child(-n + 2) { border-bottom: 1px solid #dce2e7; } }@media (max-width: 480px) { .redemption-box { align-items: stretch; flex-wrap: wrap; }.redemption-icon { display: none; }.redemption-box .el-input { width: 100%; flex-basis: 100%; }.redemption-box .el-button { width: 100%; }.usage-breakdown { grid-template-columns: 1fr; }.usage-breakdown div { border-right: 0; border-bottom: 1px solid #dce2e7; }.usage-breakdown div:last-child { border-bottom: 0; } }
</style>
