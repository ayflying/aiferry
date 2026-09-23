<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { CircleAlert, Coins, Copy, Eye, Gauge, KeyRound, Plus, Settings2, Trash2 } from '@lucide/vue'
import { ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
import type { Channel, ChannelCostResult, ChannelCredential, CostSummary, CredentialRevealStatus } from '../api/types'
import { mergeCostSummaries } from '../lib/cost'
import { showError, showSuccess, showWarning } from '../lib/error'
import { copyText } from '../lib/clipboard'
import { formatBalance, formatCost, formatTime, formatNumber } from '../lib/format'
import { channelQueryValueLabel, isUsageMode } from '../lib/channelTypeDisplay'

const props = defineProps<{ modelValue: boolean; channel?: Channel; quotaSupported?: boolean; managementKeySupported?: boolean; managementKeyPair?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; changed: []; 'query-quota': [credential: ChannelCredential] }>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const loading = ref(false)
const adding = ref(false)
const querying = ref(false)
const credentialValue = ref('')
const credentialManagementValue = ref('')
// 火山 AFP 的管理密钥是 AK/SK 两个值，弹窗内分开输入，保存时合并为 "AK:SK"。
const mgmtDialogVisible = ref(false)
const mgmtDialogTarget = ref<ChannelCredential | null>(null)
const mgmtAccessKeyInput = ref('')
const mgmtSecretKeyInput = ref('')
const mgmtSaving = ref(false)
const rows = ref<ChannelCredential[]>([])
const queryDetails = ref<ChannelCostResult['credentials']>([])
const summaries = ref<CostSummary[]>([])
// 查看完整上游密钥：先查 10 分钟邮箱验证窗口，通过后弹框展示明文；
// 明文只存在组件内存里，抽屉关闭即清空，表格行内始终只显示前缀。
const secretLoading = ref<Record<number, boolean>>({})
const secretDialogVisible = ref(false)
const secretDialogTitle = ref('')
const secretDialogValue = ref('')
const revealDialogVisible = ref(false)
const revealStatus = ref<CredentialRevealStatus | null>(null)
const revealCode = ref('')
const sendingCode = ref(false)
const verifyingCode = ref(false)
const codeCountdown = ref(0)
let codeCountdownTimer: ReturnType<typeof setInterval> | undefined
let pendingRevealId: number | null = null
const drawerSize = window.innerWidth <= 600 ? '94%' : '760px'
const usageQuery = computed(() => isUsageMode(props.channel?.costQueryType, props.channel?.costQueryMode))
// 各上游密钥可能挂在不同币种账户上，汇总卡片只展示折算合并后的一条，
// 折算到同一货币再求和才是可比口径；每把密钥的原始币种仍保留在下方明细里。
const mergedSummary = computed(() => mergeCostSummaries(summaries.value))
const queryLabel = computed(() => {
  return `查询${channelQueryValueLabel(props.channel?.costQueryType, props.channel?.costQueryMode)}`
})

watch(() => props.modelValue, (open) => {
  if (open) void load(true)
  else clearRevealState()
})
watch(() => props.channel?.id, () => {
  if (visible.value) {
    clearRevealState()
    void load(true)
  }
})
onBeforeUnmount(stopCodeCountdown)

function clearRevealState() {
  pendingRevealId = null
  revealDialogVisible.value = false
  revealCode.value = ''
  secretDialogVisible.value = false
  secretDialogTitle.value = ''
  secretDialogValue.value = ''
  secretLoading.value = {}
  stopCodeCountdown()
}

async function load(resetDetails = false) {
  if (!props.channel) return
  loading.value = true
  if (resetDetails) {
    queryDetails.value = []
    summaries.value = props.channel.costSummaries || []
  }
  try {
    rows.value = await apiGet<ChannelCredential[]>(`/channels/${props.channel.id}/credentials`)
  } catch (error) {
    showError(error, '加载上游密钥失败')
  } finally {
    loading.value = false
  }
}

async function addCredential() {
  if (!props.channel || !credentialValue.value.trim()) return
  adding.value = true
  try {
    const payload: Record<string, string> = { apiKey: credentialValue.value.trim() }
    if (credentialManagementValue.value.trim()) payload.managementKey = credentialManagementValue.value.trim()
    await apiPost(`/channels/${props.channel.id}/credentials`, payload)
    credentialValue.value = ''
    credentialManagementValue.value = ''
    showSuccess('上游密钥已追加')
    await load(true)
    emit('changed')
  } catch (error) {
    showError(error, '追加上游密钥失败')
  } finally {
    adding.value = false
  }
}

// setManagementKey 为单把凭证设置管理密钥。火山渠道（managementKeyPair）
// 弹出 AK/SK 两个独立输入框；其余类型保持单输入框弹窗。
// 传空白时确认后清除该凭证的管理密钥，回退渠道级共享。
async function setManagementKey(item: ChannelCredential) {
  if (!props.channel) return
  if (props.managementKeyPair) {
    mgmtDialogTarget.value = item
    mgmtAccessKeyInput.value = ''
    mgmtSecretKeyInput.value = ''
    mgmtDialogVisible.value = true
    return
  }
  let value: string
  try {
    const result = await ElMessageBox.prompt(
      item.hasManagementKey
        ? `该密钥已配置管理密钥，输入新值将覆盖；留空并确认则清除，清除后回退渠道级管理密钥。`
        : `为该上游账号配置管理密钥（用于查询该账号的用量/余额），留空取消。`,
      `管理密钥 · #${item.index} ${item.keyPrefix}`,
      {
        type: 'warning',
        confirmButtonText: '保存',
        cancelButtonText: '取消',
        inputPlaceholder: item.hasManagementKey ? '已配置（输入新值覆盖，留空清除）' : '粘贴上游管理密钥',
        inputType: 'password',
        inputValue: '',
      },
    )
    value = (result.value || '').trim()
  } catch (error) {
    if (error !== 'cancel') showError(error, '打开管理密钥输入失败')
    return
  }
  await saveManagementKey(item, value)
}

async function saveManagementKey(item: ChannelCredential, value: string) {
  if (!props.channel) return
  try {
    await apiPut(`/channels/${props.channel.id}/credentials/${item.id}/management-key`, { managementKey: value })
    item.hasManagementKey = value !== ''
    showSuccess(value ? '管理密钥已保存' : '管理密钥已清除，回退渠道级')
  } catch (error) {
    showError(error, '保存管理密钥失败')
  }
}

// saveManagementKeyPair 保存火山 AK/SK 对话框：两个值都必须填写；
// 两栏全空视为取消，任意一栏为空提示补全。
async function saveManagementKeyPair() {
  const item = mgmtDialogTarget.value
  if (!item) return
  const accessKey = mgmtAccessKeyInput.value.trim()
  const secretKey = mgmtSecretKeyInput.value.trim()
  if (!accessKey && !secretKey) {
    mgmtDialogVisible.value = false
    return
  }
  if (!accessKey || !secretKey) {
    showWarning('Access Key 和 Secret Key 需要都填写')
    return
  }
  mgmtSaving.value = true
  try {
    await saveManagementKey(item, `${accessKey}:${secretKey}`)
    mgmtDialogVisible.value = false
  } finally {
    mgmtSaving.value = false
  }
}

async function setStatus(item: ChannelCredential, enabled: boolean) {
  if (!props.channel) return
  try {
    await apiPut(`/channels/${props.channel.id}/credentials/${item.id}/status`, { status: enabled ? 1 : 0 })
    item.status = enabled ? 1 : 0
    if (enabled) {
      item.autoDisabled = false
      item.autoDisabledReason = ''
    }
    showSuccess(enabled ? '上游密钥已启用' : '上游密钥已停用')
    emit('changed')
  } catch (error) {
    showError(error, '更新上游密钥状态失败')
  }
}

async function remove(item: ChannelCredential) {
  if (!props.channel) return
  try {
    await ElMessageBox.confirm(`删除上游密钥 #${item.index}（${item.keyPrefix}）？编号按创建顺序固定、删除后不重排；固定使用该密钥的访问密钥会在下次请求重新选择。`, '删除上游密钥', {
      type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消',
    })
    await apiDelete(`/channels/${props.channel.id}/credentials/${item.id}`)
    showSuccess('上游密钥已删除')
    await load(true)
    emit('changed')
  } catch (error) {
    if (error !== 'cancel') showError(error, '删除上游密钥失败')
  }
}

async function queryCosts() {
  if (!props.channel) return
  querying.value = true
  try {
    const result = await apiPost<ChannelCostResult>(`/channels/${props.channel.id}/costs/query`, {})
    queryDetails.value = result.credentials || []
    summaries.value = result.summaries || []
    const failures = queryDetails.value.filter((item) => item.error).length
    const valueLabel = channelQueryValueLabel(props.channel?.costQueryType, props.channel?.costQueryMode)
    showSuccess(failures ? `${valueLabel}查询完成，${failures} 个密钥失败` : `${valueLabel}查询完成`)
    await load(false)
    emit('changed')
  } catch (error) {
    showError(error, usageQuery.value ? '查询上游用量失败' : '查询上游费用失败')
  } finally {
    querying.value = false
  }
}

function statusText(item: ChannelCredential) {
  if (item.autoDisabled) return '自动禁用'
  return item.status === 1 ? '启用' : '手动停用'
}

function autoDisabledDetail(item: ChannelCredential) {
  const lines = [item.autoDisabledReason || '该密钥被系统自动禁用']
  if (item.autoDisabledAt) lines.push(`禁用时间：${formatTime(item.autoDisabledAt)}`)
  return lines.join('\n')
}

function costDetail(item: ChannelCredential) {
  return queryDetails.value.find((detail) => detail.credentialId === item.id)
}

function secretLabel(item: ChannelCredential) {
  return `#${item.index} ${item.keyPrefix}`
}

// openSecret 查看完整上游密钥：走 10 分钟验证窗口，通过后弹框展示。
async function openSecret(item: ChannelCredential) {
  if (!props.channel) return
  secretLoading.value = { ...secretLoading.value, [item.id]: true }
  try {
    const status = await apiGet<CredentialRevealStatus>('/credential-reveal/status')
    if (status.verified) {
      await showSecretDialog(item)
      return
    }
    revealStatus.value = status
    revealCode.value = ''
    pendingRevealId = item.id
    revealDialogVisible.value = true
    // 打开即自动发一次码，少一步点击。
    if (status.emailReady) void sendRevealCode()
  } catch (error) {
    showError(error, '检查邮箱验证状态失败')
  } finally {
    secretLoading.value = { ...secretLoading.value, [item.id]: false }
  }
}

async function showSecretDialog(item: ChannelCredential) {
  if (!props.channel) return
  secretLoading.value = { ...secretLoading.value, [item.id]: true }
  try {
    const result = await apiGet<{ key: string }>(`/channels/${props.channel.id}/credentials/${item.id}/secret`)
    secretDialogTitle.value = secretLabel(item)
    secretDialogValue.value = result.key
    secretDialogVisible.value = true
  } catch (error) {
    showError(error, '查看完整密钥失败')
  } finally {
    secretLoading.value = { ...secretLoading.value, [item.id]: false }
  }
}

async function copySecretDialog() {
  if (!secretDialogValue.value) return
  try {
    await copyText(secretDialogValue.value)
    showSuccess('完整密钥已复制')
  } catch (error) {
    showError(error, '复制完整密钥失败')
  }
}

async function sendRevealCode() {
  if (sendingCode.value || codeCountdown.value > 0) return
  sendingCode.value = true
  try {
    revealStatus.value = await apiPost<CredentialRevealStatus>('/credential-reveal/code')
    showSuccess('验证码已发送，请查收邮箱')
    startCodeCountdown(60)
  } catch (error) {
    showError(error, '发送验证码失败')
  } finally {
    sendingCode.value = false
  }
}

async function verifyRevealCode() {
  const code = revealCode.value.trim()
  if (!/^\d{6}$/.test(code)) {
    showWarning('请输入 6 位验证码')
    return
  }
  verifyingCode.value = true
  try {
    revealStatus.value = await apiPost<CredentialRevealStatus>('/credential-reveal/verify', { code })
    revealDialogVisible.value = false
    revealCode.value = ''
    const item = rows.value.find((row) => row.id === pendingRevealId)
    pendingRevealId = null
    if (item) await showSecretDialog(item)
  } catch (error) {
    showError(error, '验证码校验失败')
  } finally {
    verifyingCode.value = false
  }
}

function startCodeCountdown(seconds: number) {
  stopCodeCountdown()
  codeCountdown.value = seconds
  codeCountdownTimer = setInterval(() => {
    codeCountdown.value -= 1
    if (codeCountdown.value <= 0) stopCodeCountdown()
  }, 1000)
}

function stopCodeCountdown() {
  if (codeCountdownTimer) {
    clearInterval(codeCountdownTimer)
    codeCountdownTimer = undefined
  }
  codeCountdown.value = 0
}
</script>

<template>
  <el-drawer v-model="visible" :title="`上游密钥 · ${channel?.name || ''}`" :size="drawerSize" destroy-on-close>
    <div class="credential-toolbar">
      <div class="credential-add">
        <el-input v-model="credentialValue" type="password" show-password autocomplete="new-password" placeholder="追加上游推理密钥" @keyup.enter="addCredential" />
        <el-input v-if="props.managementKeySupported" v-model="credentialManagementValue" type="password" show-password autocomplete="new-password" :placeholder="props.managementKeyPair ? '管理密钥 Secret Key（可选，格式 AK:SK）' : '管理密钥（可选，按账号查用量）'" @keyup.enter="addCredential" />
        <el-button type="primary" :icon="Plus" :loading="adding" :disabled="!credentialValue.trim()" @click="addCredential">追加</el-button>
      </div>
      <el-button :icon="Coins" :loading="querying" :disabled="channel?.costQueryMode === 'none'" @click="queryCosts">{{ queryLabel }}</el-button>
    </div>

    <div v-if="mergedSummary" class="cost-summary-grid">
      <div class="summary-item">
        <strong>{{ mergedSummary.currency }}</strong>
        <span v-if="!usageQuery && mergedSummary.usedAmount !== undefined">已用 {{ formatCost(mergedSummary.usedAmount, mergedSummary.currency) }}</span>
        <span v-if="!usageQuery && mergedSummary.remainingAmount !== undefined">余额 {{ formatBalance(mergedSummary.remainingAmount, mergedSummary.currency) }}</span>
        <span v-if="mergedSummary.usage !== undefined || (usageQuery && mergedSummary.usedAmount !== undefined)">{{ mergedSummary.usageType || '用量' }} {{ formatNumber(mergedSummary.usage ?? mergedSummary.usedAmount) }} {{ mergedSummary.usageUnit || 'kToken' }}<small v-if="mergedSummary.usageDimension"> · {{ mergedSummary.usageDimension }}</small></span>
      </div>
    </div>

    <div v-loading="loading" class="credential-table">
      <el-table :data="rows" row-key="id" size="small">
        <el-table-column label="上游密钥" min-width="200"><template #default="{ row }"><span class="mono key-prefix"><el-tooltip content="固定序号：按创建顺序编号（含已删密钥占位），与用量明细的「渠道 #N」一致；删除后不重排" placement="top"><span class="cred-index">#{{ row.index }}</span></el-tooltip><KeyRound :size="14" />{{ row.keyPrefix }}<el-tooltip v-if="props.managementKeySupported && row.hasManagementKey" content="已配置该账号的管理密钥"><span class="mgmt-badge">管</span></el-tooltip></span></template></el-table-column>
        <el-table-column label="状态" min-width="156"><template #default="{ row }"><el-tooltip v-if="row.autoDisabled" :content="autoDisabledDetail(row)" placement="top-start"><div class="credential-status"><span class="status-dot warning">自动禁用</span><small v-if="row.autoDisabledAt">{{ formatTime(row.autoDisabledAt) }}</small></div></el-tooltip><span v-else class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ statusText(row) }}</span></template></el-table-column>
        <el-table-column :label="usageQuery ? '用量与额度' : '费用与余额'" min-width="200"><template #default="{ row }"><div class="cost-state"><template v-if="costDetail(row)?.error"><span class="danger-text">{{ costDetail(row)?.error }}</span></template><template v-else><span v-if="!usageQuery && row.lastCostUsed !== undefined">已用 {{ formatCost(row.lastCostUsed, row.lastCostCurrency) }}</span><span v-if="!usageQuery && row.lastCostRemaining !== undefined">余额 {{ formatBalance(row.lastCostRemaining, row.lastCostCurrency) }}</span><span v-if="usageQuery && (row.lastCostUsage !== undefined || row.lastCostUsed !== undefined)">{{ row.lastCostUsageType || '用量' }} {{ formatNumber(row.lastCostUsage ?? row.lastCostUsed) }} {{ row.lastCostUsageUnit || 'kToken' }}<small v-if="row.lastCostUsageDimension"> · {{ row.lastCostUsageDimension }}</small></span><small v-if="row.lastCostAt">{{ formatTime(row.lastCostAt) }}</small><span v-if="row.lastCostUsed === undefined && row.lastCostRemaining === undefined && row.lastCostUsage === undefined" class="muted">尚未查询</span></template></div></template></el-table-column>
        <el-table-column label="启用" width="76" align="center"><template #default="{ row }"><el-switch :model-value="row.status === 1" @update:model-value="setStatus(row, $event)" /></template></el-table-column>
        <el-table-column label="操作" width="150" align="center"><template #default="{ row }"><div class="row-actions"><el-tooltip content="查看完整密钥"><button class="icon-button" type="button" :aria-label="`查看 ${row.keyPrefix}`" :disabled="secretLoading[row.id]" @click="openSecret(row)"><Eye :size="16" /></button></el-tooltip><el-tooltip v-if="props.managementKeySupported" content="设置/清除该账号的管理密钥"><button class="icon-button" type="button" :aria-label="`设置 ${row.keyPrefix} 管理密钥`" @click="setManagementKey(row)"><Settings2 :size="16" /></button></el-tooltip><el-tooltip v-if="props.quotaSupported" content="查询该密钥的套餐额度"><button class="icon-button" type="button" :aria-label="`查询 ${row.keyPrefix} 额度`" @click="emit('query-quota', row)"><Gauge :size="16" /></button></el-tooltip><el-tooltip content="删除上游密钥"><button class="icon-button danger" type="button" :aria-label="`删除 ${row.keyPrefix}`" @click="remove(row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
      </el-table>
      <div v-if="!loading && !rows.length" class="credential-empty"><CircleAlert :size="18" /><span>当前渠道没有可管理的上游密钥</span></div>
    </div>

    <div v-if="props.managementKeySupported && queryDetails.some(item => item.shared)" class="shared-balance">
      <strong>管理密钥共享余额</strong>
      <span v-for="item in queryDetails.filter(detail => detail.shared)" :key="item.queriedAt">{{ item.remainingAmount === undefined ? '未返回余额' : formatBalance(item.remainingAmount, item.currency) }}</span>
    </div>

    <el-dialog v-model="mgmtDialogVisible" :title="`管理密钥（AK/SK） · #${mgmtDialogTarget?.index ?? ''} ${mgmtDialogTarget?.keyPrefix || ''}`" width="460px" append-to-body>
      <el-alert v-if="mgmtDialogTarget?.hasManagementKey" type="warning" :closable="false" show-icon title="该上游账号已配置管理密钥，保存后将覆盖；两栏全空保存则取消。" class="mgmt-dialog-alert" />
      <el-form label-position="top" @submit.prevent="saveManagementKeyPair">
        <el-form-item label="Access Key（访问密钥 ID）"><el-input v-model="mgmtAccessKeyInput" placeholder="AK，例如 AKTPxxxxxxxx" autocomplete="off" /></el-form-item>
        <el-form-item label="Secret Key（访问密钥）"><el-input v-model="mgmtSecretKeyInput" type="password" show-password placeholder="SK" autocomplete="new-password" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="mgmtDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="mgmtSaving" @click="saveManagementKeyPair">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="secretDialogVisible" :title="`完整密钥 · ${secretDialogTitle}`" width="560px" append-to-body @closed="secretDialogValue = ''">
      <div class="secret-dialog-body">
        <div class="secret-dialog-value mono">{{ secretDialogValue }}</div>
        <el-button type="primary" :icon="Copy" @click="copySecretDialog">复制</el-button>
      </div>
      <template #footer>
        <el-button @click="secretDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="revealDialogVisible" title="邮箱验证 · 查看密钥明文" width="460px" append-to-body @closed="revealCode = ''">
      <el-alert type="info" :closable="false" show-icon class="reveal-alert" :title="revealStatus?.emailReady ? `验证码将发送至 ${revealStatus.emailMasked}` : '尚未填写邮箱，请先在「个人设置」中填写邮箱后再发送验证码。'" />
      <el-form label-position="top" @submit.prevent="verifyRevealCode">
        <el-form-item label="验证码">
          <div class="reveal-code-row">
            <el-input v-model="revealCode" placeholder="6 位验证码" maxlength="6" autocomplete="one-time-code" @keyup.enter="verifyRevealCode" />
            <el-button :loading="sendingCode" :disabled="!revealStatus?.emailReady || codeCountdown > 0" @click="sendRevealCode">{{ codeCountdown > 0 ? `${codeCountdown}s 后重发` : '发送验证码' }}</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="revealDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="verifyingCode" @click="verifyRevealCode">验证并查看</el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<style scoped>
.credential-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }.credential-add { display: flex; min-width: 0; flex: 1; gap: 8px; }.mgmt-badge { display: inline-flex; align-items: center; justify-content: center; margin-left: 2px; padding: 0 5px; border: 1px solid #b8d4ea; border-radius: 4px; color: #2a6f9e; background: #eef6fc; font-size: 10px; line-height: 16px; }.cost-summary-grid { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 14px; }.summary-item { display: flex; align-items: center; gap: 9px; padding: 7px 10px; border: 1px solid #dce2e7; border-radius: 6px; background: #fff; font-size: 11px; }.summary-item strong { color: #15202b; font-family: 'JetBrains Mono', monospace; }.cost-state { display: flex; min-width: 0; flex-direction: column; gap: 2px; font-size: 11px; }.cost-state small, .credential-status small { color: #7b8792; }.credential-status { display: flex; min-width: 0; flex-direction: column; gap: 2px; }.key-prefix { display: inline-flex; align-items: center; gap: 6px; }.reveal-alert { margin-bottom: 12px; }.reveal-code-row { display: flex; width: 100%; gap: 8px; }.reveal-code-row .el-input { flex: 1; }.secret-dialog-body { display: flex; flex-direction: column; gap: 12px; }.secret-dialog-value { padding: 10px 12px; border: 1px solid #d5dde3; border-radius: 6px; background: #f4f6f8; color: #15202b; font-size: 13px; line-height: 1.5; word-break: break-all; white-space: pre-wrap; }.cred-index { display: inline-flex; align-items: center; padding: 0 4px; border: 1px solid #d5dde3; border-radius: 4px; color: #5b6a77; background: #f4f6f8; font-size: 10px; line-height: 16px; cursor: help; }.key-prefix .cred-index + svg { margin-left: -2px; }.mgmt-dialog-alert { margin-bottom: 12px; }.row-actions { display: inline-flex; align-items: center; gap: 6px; }.credential-empty { display: flex; min-height: 170px; align-items: center; justify-content: center; gap: 8px; color: #7b8792; font-size: 12px; }.shared-balance { display: flex; align-items: center; gap: 8px; margin-top: 14px; padding: 10px; border: 1px solid #c6dae9; border-radius: 6px; color: #40505f; background: #f4f9fd; font-size: 12px; }
</style>
