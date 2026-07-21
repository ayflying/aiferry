<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Clock3, Database, Gauge, HardDrive, Info, Mail, Route, Send, ShieldAlert, ShieldCheck } from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { apiGet, apiPost, apiPut } from '../api/client'
import type { BaseSettings, MailSettings, SensitiveWordSettings, SystemInformationSettings, SystemResilienceSettings } from '../api/types'
import { showError } from '../lib/error'
import { setDisplayTimeZone } from '../lib/format'
import { useSystemStore } from '../stores/system'

interface SystemInfo { name: string; adminMode: string; database: string; cache: string; apiVersion: string }
type SettingsTab = 'overview' | 'basic' | 'information' | 'resilience' | 'sensitive' | 'mail'

const tabLoading = reactive<Record<SettingsTab, boolean>>({ overview: false, basic: false, information: false, resilience: false, sensitive: false, mail: false })
const saving = ref(false)
const informationSaving = ref(false)
const sensitiveSaving = ref(false)
const mailSaving = ref(false)
const testSending = ref(false)
const activeTab = ref<SettingsTab>('overview')
const info = ref<SystemInfo>()
const basicForm = reactive({ timeZone: 'Asia/Shanghai' })
const timeZoneOptions = [
  { label: '中国标准时间 (UTC+08:00)', value: 'Asia/Shanghai' },
  { label: '协调世界时 (UTC)', value: 'UTC' },
  { label: '日本标准时间 (UTC+09:00)', value: 'Asia/Tokyo' },
  { label: '欧洲中部时间', value: 'Europe/Berlin' },
  { label: '英国时间', value: 'Europe/London' },
  { label: '美国东部时间', value: 'America/New_York' },
  { label: '美国西部时间', value: 'America/Los_Angeles' },
]
const system = useSystemStore()
const informationForm = reactive<SystemInformationSettings>({
  systemName: 'AiFerry', serverUrl: '', logoUrl: '', footer: '', about: '', homeContent: '', userAgreement: '', privacyPolicy: '',
})
const form = reactive({
  maxFailoverAttempts: 3,
  retryStatusCodes: '401,403,404,408,429,500-599',
  streamFirstByteTimeoutSeconds: 60,
  streamIdleTimeoutSeconds: 180,
  nonStreamTimeoutSeconds: 600,
  healthCheckEnabled: false,
  healthCheckMode: 'passive' as SystemResilienceSettings['healthCheckMode'],
  healthCheckIntervalMinutes: 5,
  recoveryEnabled: true,
  autoDisableEnabled: true,
  disableLatencySeconds: 120,
  disableStatusCodes: '401,429',
  failureKeywordsText: '',
})
const sensitiveForm = reactive({
  enabled: false,
  checkUserPrompt: false,
  keywordsText: '',
})
const mailForm = reactive({
  enabled: false,
  host: '',
  port: 587,
  username: '',
  password: '',
  passwordConfigured: false,
  from: '',
  security: 'starttls' as MailSettings['security'],
  threshold: 5,
  subjectTemplate: '',
  bodyTemplate: '',
})
const testRecipient = ref('')

const sectionMeta = computed(() => ({
  overview: { title: '运行概览', description: '实例与依赖状态' },
  basic: { title: '基础设置', description: '全局时区与时间展示规则' },
  information: { title: '系统信息', description: '应用身份、公开地址与站点内容' },
  resilience: { title: '路由可靠性', description: '故障转移、健康检查与自动禁用' },
  sensitive: { title: '敏感词', description: '在请求到达上游模型前检查用户输入' },
  mail: { title: '邮件提醒', description: '按模型使用触发的余额提醒与 SMTP 投递配置' },
}[activeTab.value]))
const activeTabLoading = computed(() => tabLoading[activeTab.value])

function applySettings(settings: SystemResilienceSettings) {
  Object.assign(form, {
    ...settings,
    healthCheckMode: settings.healthCheckMode,
    failureKeywordsText: settings.failureKeywords.join('\n'),
  })
}

function applySystemInformation(settings: SystemInformationSettings) {
  Object.assign(informationForm, settings)
}

function applyMailSettings(settings: MailSettings) {
  Object.assign(mailForm, { ...settings, password: '' })
}

function applySensitiveWordSettings(settings: SensitiveWordSettings) {
  Object.assign(sensitiveForm, {
    enabled: settings.enabled,
    checkUserPrompt: settings.checkUserPrompt,
    keywordsText: settings.keywords.join('\n'),
  })
}

async function loadOverview() {
  tabLoading.overview = true
  try {
    info.value = await apiGet<SystemInfo>('/system')
  } catch (error) { showError(error, '加载运行概览失败') } finally { tabLoading.overview = false }
}

async function loadBasic() {
  tabLoading.basic = true
  try {
    Object.assign(basicForm, await apiGet<BaseSettings>('/system/basic'))
  } catch (error) { showError(error, '加载基础设置失败') } finally { tabLoading.basic = false }
}

async function loadResilience() {
  tabLoading.resilience = true
  try {
    applySettings(await apiGet<SystemResilienceSettings>('/system/settings'))
  } catch (error) { showError(error, '加载路由可靠性设置失败') } finally { tabLoading.resilience = false }
}

async function loadSystemInformation() {
  tabLoading.information = true
  try {
    applySystemInformation(await apiGet<SystemInformationSettings>('/system/information'))
  } catch (error) { showError(error, '加载系统信息失败') } finally { tabLoading.information = false }
}

async function loadMail() {
  tabLoading.mail = true
  try {
    applyMailSettings(await apiGet<MailSettings>('/system/mail'))
  } catch (error) { showError(error, '加载邮件提醒设置失败') } finally { tabLoading.mail = false }
}

async function loadSensitiveWords() {
  tabLoading.sensitive = true
  try {
    applySensitiveWordSettings(await apiGet<SensitiveWordSettings>('/system/sensitive-words'))
  } catch (error) { showError(error, '加载敏感词设置失败') } finally { tabLoading.sensitive = false }
}

function loadTab(tab: SettingsTab) {
  return { overview: loadOverview, basic: loadBasic, information: loadSystemInformation, resilience: loadResilience, sensitive: loadSensitiveWords, mail: loadMail }[tab]()
}

function handleTabChange(tab: string | number) {
  void loadTab(tab as SettingsTab)
}

async function saveReliability() {
  saving.value = true
  try {
    const settings = await apiPut<SystemResilienceSettings>('/system/settings', {
      maxFailoverAttempts: form.maxFailoverAttempts,
      retryStatusCodes: form.retryStatusCodes,
      streamFirstByteTimeoutSeconds: form.streamFirstByteTimeoutSeconds,
      streamIdleTimeoutSeconds: form.streamIdleTimeoutSeconds,
      nonStreamTimeoutSeconds: form.nonStreamTimeoutSeconds,
      healthCheckEnabled: form.healthCheckEnabled,
      healthCheckMode: form.healthCheckMode,
      healthCheckIntervalMinutes: form.healthCheckIntervalMinutes,
      recoveryEnabled: form.recoveryEnabled,
      autoDisableEnabled: form.autoDisableEnabled,
      disableLatencySeconds: form.disableLatencySeconds,
      disableStatusCodes: form.disableStatusCodes,
      failureKeywords: form.failureKeywordsText.split('\n').map((item) => item.trim()).filter(Boolean),
    })
    applySettings(settings)
    ElMessage.success('系统设置已保存')
  } catch (error) { showError(error, '保存系统设置失败') } finally { saving.value = false }
}

async function saveBasic() {
  saving.value = true
  try {
    const settings = await apiPut<BaseSettings>('/system/basic', { timeZone: basicForm.timeZone })
    Object.assign(basicForm, settings)
    setDisplayTimeZone(settings.timeZone)
    ElMessage.success('基础设置已保存')
  } catch (error) { showError(error, '保存基础设置失败') } finally { saving.value = false }
}

async function saveSystemInformation() {
  informationSaving.value = true
  try {
    const settings = await apiPut<SystemInformationSettings>('/system/information', { ...informationForm })
    applySystemInformation(settings)
    system.apply(settings)
    ElMessage.success('系统信息已保存')
  } catch (error) { showError(error, '保存系统信息失败') } finally { informationSaving.value = false }
}

async function saveMail() {
  mailSaving.value = true
  try {
    const password = mailForm.password.trim()
    const settings = await apiPut<MailSettings>('/system/mail', {
      enabled: mailForm.enabled,
      host: mailForm.host,
      port: mailForm.port,
      username: mailForm.username,
      password: password || undefined,
      from: mailForm.from,
      security: mailForm.security,
      threshold: mailForm.threshold,
      subjectTemplate: mailForm.subjectTemplate,
      bodyTemplate: mailForm.bodyTemplate,
    })
    applyMailSettings(settings)
    ElMessage.success('邮件设置已保存')
  } catch (error) { showError(error, '保存邮件设置失败') } finally { mailSaving.value = false }
}

async function saveSensitiveWords() {
  sensitiveSaving.value = true
  try {
    const settings = await apiPut<SensitiveWordSettings>('/system/sensitive-words', {
      enabled: sensitiveForm.enabled,
      checkUserPrompt: sensitiveForm.checkUserPrompt,
      keywords: sensitiveForm.keywordsText.split('\n').map((item) => item.trim()).filter(Boolean),
    })
    applySensitiveWordSettings(settings)
    ElMessage.success('敏感词设置已保存')
  } catch (error) { showError(error, '保存敏感词设置失败') } finally { sensitiveSaving.value = false }
}

async function sendTestMail() {
  testSending.value = true
  try {
    await apiPost<Record<string, never>>('/system/mail/test', { recipient: testRecipient.value })
    ElMessage.success('测试邮件已发送')
  } catch (error) { showError(error, '发送测试邮件失败') } finally { testSending.value = false }
}

function saveActive() {
  if (activeTab.value === 'basic') return saveBasic()
  if (activeTab.value === 'information') return saveSystemInformation()
  if (activeTab.value === 'basic') return saveBasic()
  if (activeTab.value === 'resilience') return saveReliability()
  if (activeTab.value === 'sensitive') return saveSensitiveWords()
  if (activeTab.value === 'mail') return saveMail()
}

onMounted(loadOverview)
</script>

<template>
  <div v-loading="activeTabLoading" class="page-stack settings-page">
    <el-tabs v-model="activeTab" class="settings-tabs sticky-page-tabs" @tab-change="handleTabChange">
      <el-tab-pane name="overview"><template #label><span class="tab-label"><Activity :size="15" />概览</span></template></el-tab-pane>
      <el-tab-pane name="basic"><template #label><span class="tab-label"><Clock3 :size="15" />基础设置</span></template></el-tab-pane>
      <el-tab-pane name="information"><template #label><span class="tab-label"><Info :size="15" />系统信息</span></template></el-tab-pane>
      <el-tab-pane name="resilience"><template #label><span class="tab-label"><Route :size="15" />路由可靠性</span></template></el-tab-pane>
      <el-tab-pane name="sensitive"><template #label><span class="tab-label"><ShieldAlert :size="15" />敏感词</span></template></el-tab-pane>
      <el-tab-pane name="mail"><template #label><span class="tab-label"><Mail :size="15" />邮件提醒</span></template></el-tab-pane>
    </el-tabs>

    <div class="page-toolbar settings-toolbar">
      <div><h1>{{ sectionMeta.title }}</h1><span class="muted">{{ sectionMeta.description }}</span></div>
      <div class="spacer" />
    </div>

    <template v-if="activeTab === 'overview'">
      <section class="settings-band auth-band"><ShieldCheck :size="22" /><div><strong>Casdoor 单点登录已启用</strong><span>管理端访问由统一身份与用户组策略保护。</span></div></section>
      <section class="settings-band"><Database :size="21" /><div class="settings-title"><strong>数据存储</strong><span>业务事实与用量账本</span></div><div class="settings-value"><span>{{ info?.database || '—' }}</span><small>已配置</small></div></section>
      <section class="settings-band"><HardDrive :size="21" /><div class="settings-title"><strong>缓存与临时状态</strong><span>密钥缓存、故障计数与短时冷却</span></div><div class="settings-value"><span>{{ info?.cache || '—' }}</span><small>已配置</small></div></section>
      <section class="settings-grid"><div><span>产品</span><strong>{{ info?.name || '—' }}</strong></div><div><span>管理模式</span><strong>{{ info?.adminMode || '—' }}</strong></div><div><span>中转 API</span><strong>{{ info?.apiVersion || '—' }}</strong></div><div><span>支持范围</span><strong>OpenAI 文本核心</strong></div></section>
    </template>

    <template v-else-if="activeTab === 'basic'">
      <section class="settings-section"><div class="section-heading"><div><h2>系统时区</h2><span>历史调用时间固定按北京时间解释；切换后按所选时区重新展示历史记录和统计，不会修改历史发生时间。</span></div><Clock3 :size="19" /></div><el-form label-position="top" class="settings-form"><el-form-item label="时区"><el-select v-model="basicForm.timeZone" filterable style="max-width: 420px"><el-option v-for="item in timeZoneOptions" :key="item.value" :label="item.label" :value="item.value" /></el-select></el-form-item><p class="field-hint">保存后当前页面立即刷新时间格式；其他已打开的页面刷新后应用新时区。</p></el-form></section>
    </template>

    <template v-else-if="activeTab === 'information'">
      <section class="settings-section"><div class="section-heading"><div><h2>应用身份</h2><span>系统名称和徽标会显示在登录页、导航和浏览器标题中。</span></div><Info :size="19" /></div><el-form label-position="top" class="settings-form"><el-form-item label="系统名称"><el-input v-model="informationForm.systemName" maxlength="96" show-word-limit /></el-form-item><p class="field-hint">在整个应用程序中显示的名称。</p><div class="form-grid"><el-form-item label="服务器地址"><el-input v-model="informationForm.serverUrl" placeholder="https://yourdomain.com" inputmode="url" /></el-form-item><el-form-item label="徽标 URL"><el-input v-model="informationForm.logoUrl" placeholder="https://example.com/logo.png" inputmode="url" /></el-form-item></div><p class="field-hint">服务器地址用于 Casdoor 回调和外部集成；徽标为空时使用内置图标。</p></el-form></section>
      <section class="settings-section"><div class="section-heading"><div><h2>页面内容</h2><span>页脚按纯文本显示；其他内容支持 Markdown、HTML 或指定的完整 URL。</span></div></div><el-form label-position="top" class="settings-form"><el-form-item label="页脚"><el-input v-model="informationForm.footer" type="textarea" :rows="3" maxlength="4096" show-word-limit /></el-form-item><p class="field-hint">显示在页面底部的页脚文本。</p><el-form-item label="关于"><el-input v-model="informationForm.about" type="textarea" :rows="6" spellcheck="false" /></el-form-item><p class="field-hint">支持 Markdown 或 HTML；完整 HTTP(S) URL 会以受限页面嵌入。</p><el-form-item label="首页内容"><el-input v-model="informationForm.homeContent" type="textarea" :rows="6" spellcheck="false" /></el-form-item><p class="field-hint">显示在登录页下方，支持 Markdown。</p><el-form-item label="用户协议"><el-input v-model="informationForm.userAgreement" type="textarea" :rows="6" spellcheck="false" /></el-form-item><p class="field-hint">留空以不要求确认；可填写 Markdown、HTML 或完整 URL。</p><el-form-item label="隐私政策"><el-input v-model="informationForm.privacyPolicy" type="textarea" :rows="6" spellcheck="false" /></el-form-item><p class="field-hint">留空以不要求确认；可填写 Markdown、HTML 或完整 URL。</p></el-form></section>
    </template>

    <template v-else-if="activeTab === 'resilience'">
      <section class="settings-section"><div class="section-heading"><div><h2>故障转移</h2><span>单次请求会按路由顺序切换不同渠道</span></div><Gauge :size="19" /></div><el-form label-position="top" class="settings-form"><div class="form-grid"><el-form-item label="最大尝试渠道数"><el-input-number v-model="form.maxFailoverAttempts" :min="1" :max="10" controls-position="right" /></el-form-item><el-form-item label="可故障转移状态码"><el-input v-model="form.retryStatusCodes" placeholder="401,429,500-599" /></el-form-item></div><p class="field-hint">状态码支持逗号分隔和包含范围，例如 401,429,500-599。</p></el-form></section>
      <section class="settings-section"><div class="section-heading"><div><h2>超时配置</h2><span>请求超时会按现有故障转移与密钥级冷却规则处理。</span></div></div><el-form label-position="top" class="settings-form"><div class="timeout-grid"><el-form-item label="流式首字节超时"><el-input-number v-model="form.streamFirstByteTimeoutSeconds" :min="1" :max="120" controls-position="right" /><p class="field-hint">等待首个数据块的最大时间，范围 1-120 秒。</p></el-form-item><el-form-item label="流式静默超时"><el-input-number v-model="form.streamIdleTimeoutSeconds" :min="0" :max="600" controls-position="right" /><p class="field-hint">数据块之间的最大间隔，范围 0-600 秒，填 0 禁用。</p></el-form-item><el-form-item label="非流式超时"><el-input-number v-model="form.nonStreamTimeoutSeconds" :min="60" :max="1200" controls-position="right" /><p class="field-hint">非流式请求的总超时时间，范围 60-1200 秒。</p></el-form-item></div></el-form></section>
      <section class="settings-section probe-settings"><div class="section-heading"><div><h2>后台渠道探测</h2><span>使用已启用模型执行最小请求，不保存提示词或响应正文。</span></div><el-switch v-model="form.healthCheckEnabled" /></div><div class="probe-controls"><div class="probe-interval"><strong>检查间隔</strong><el-input-number v-model="form.healthCheckIntervalMinutes" :disabled="!form.healthCheckEnabled" :min="1" :max="1440" controls-position="right" /><small>分钟</small></div><div class="probe-option"><div><strong>仅被动恢复</strong><span>只探测自动禁用的渠道</span></div><el-switch :model-value="form.healthCheckMode === 'passive'" :disabled="!form.healthCheckEnabled" @update:model-value="form.healthCheckMode = $event ? 'passive' : 'all'" /></div><div class="probe-option"><div><strong>成功后自动恢复</strong><span>探测成功时恢复自动禁用渠道</span></div><el-switch v-model="form.recoveryEnabled" /></div></div></section>
      <section class="settings-section"><div class="section-heading"><div><h2>上游异常自动下线</h2><span>命中任一规则后渠道停止参与路由，并保存触发原因。</span></div><el-switch v-model="form.autoDisableEnabled" /></div><el-form label-position="top" class="settings-form"><div class="form-grid"><el-form-item label="慢响应阈值（秒）"><el-input-number v-model="form.disableLatencySeconds" :disabled="!form.autoDisableEnabled" :min="1" :max="3600" controls-position="right" /></el-form-item><el-form-item label="自动禁用状态码"><el-input v-model="form.disableStatusCodes" :disabled="!form.autoDisableEnabled" placeholder="401,429" /></el-form-item></div><el-form-item label="失败关键词"><el-input v-model="form.failureKeywordsText" :disabled="!form.autoDisableEnabled" type="textarea" :rows="10" spellcheck="false" placeholder="每行一个关键词" /></el-form-item><p class="field-hint">关键词不区分大小写；状态码支持逗号分隔和包含范围。</p></el-form></section>
    </template>

    <template v-else-if="activeTab === 'sensitive'">
      <section class="settings-section">
        <div class="section-heading"><div><h2>请求过滤</h2><span>按配置检查用户提交的文本内容。</span></div><ShieldAlert :size="19" /></div>
        <div class="setting-switch sensitive-switch"><div><strong>启用过滤</strong><span>检测到敏感关键词时阻止消息。</span></div><el-switch v-model="sensitiveForm.enabled" /></div>
        <div class="setting-switch sensitive-switch"><div><strong>检查用户提示</strong><span>启用后，提示将在到达上游模型之前被扫描。</span></div><el-switch v-model="sensitiveForm.checkUserPrompt" /></div>
        <el-form label-position="top" class="settings-form"><el-form-item label="已阻止的关键词"><el-input v-model="sensitiveForm.keywordsText" type="textarea" :rows="12" spellcheck="false" placeholder="每行一个关键词" /></el-form-item><p class="field-hint">每行代表一个关键词。留空以禁用列表，但保留开关状态。</p></el-form>
      </section>
    </template>

    <template v-else>
      <section class="settings-section"><div class="section-heading"><div><h2>余额提醒</h2><span>用户在 24 小时内有模型使用且余额低于阈值时，最多发送一次提醒；没有使用不发送。</span></div><el-switch v-model="mailForm.enabled" /></div><el-form label-position="top" class="settings-form"><el-form-item label="提醒阈值（美元）"><el-input-number v-model="mailForm.threshold" :min="0" :max="1000000" :precision="6" controls-position="right" /></el-form-item><div class="form-subsection"><strong>提醒模板</strong><span>可使用 {nickname}、{balance} 和 {threshold} 作为变量。</span></div><el-form-item label="邮件主题"><el-input v-model="mailForm.subjectTemplate" /></el-form-item><el-form-item label="邮件正文"><el-input v-model="mailForm.bodyTemplate" type="textarea" :rows="8" spellcheck="false" /></el-form-item></el-form></section>
      <section class="settings-section"><div class="section-heading"><div><h2>SMTP 服务配置</h2><span>密码只会加密保存，读取时不会返回原始内容。</span></div><Mail :size="19" /></div><el-form label-position="top" class="settings-form"><div class="form-grid"><el-form-item label="SMTP 主机"><el-input v-model="mailForm.host" placeholder="smtp.example.com" /></el-form-item><el-form-item label="端口"><el-input-number v-model="mailForm.port" :min="1" :max="65535" controls-position="right" /></el-form-item></div><div class="form-grid"><el-form-item label="加密方式"><el-select v-model="mailForm.security"><el-option label="STARTTLS" value="starttls" /><el-option label="TLS" value="tls" /><el-option label="不加密" value="none" /></el-select></el-form-item><el-form-item label="用户名"><el-input v-model="mailForm.username" autocomplete="username" /></el-form-item></div><div class="form-grid"><el-form-item :label="mailForm.passwordConfigured ? '密码（留空不修改）' : '密码'"><el-input v-model="mailForm.password" type="password" show-password autocomplete="new-password" /></el-form-item><el-form-item label="发件人邮箱"><el-input v-model="mailForm.from" placeholder="noreply@example.com" /></el-form-item></div></el-form><div class="setting-switch smtp-test"><div><strong>发送测试邮件</strong><span>使用当前已保存的 SMTP 配置投递。</span></div><div class="mail-test-actions"><el-input v-model="testRecipient" type="email" placeholder="recipient@example.com" /><el-button type="primary" :icon="Send" :loading="testSending" :disabled="testSending" @click="sendTestMail">发送</el-button></div></div></section>
    </template>

    <div v-if="activeTab !== 'overview'" class="settings-save-actions"><el-button type="primary" :loading="saving || informationSaving || sensitiveSaving || mailSaving" @click="saveActive">保存更改</el-button></div>
  </div>
</template>

<style scoped>
.settings-page { width: 100%; }.settings-tabs :deep(.el-tabs__nav-wrap::after) { background: #dce2e7; }.tab-label { display: inline-flex; align-items: center; gap: 7px; }.settings-toolbar { display: flex; min-height: 54px; align-items: center; gap: 10px; padding: 16px 0 18px; border-bottom: 1px solid #dce2e7; }.settings-toolbar h1 { margin: 0 0 4px; color: #15202b; font-size: 16px; font-weight: 650; }.spacer { flex: 1; }.settings-band { display: grid; min-height: 78px; grid-template-columns: auto 1fr auto; align-items: center; gap: 14px; padding: 16px 0; border-bottom: 1px solid #dce2e7; }.auth-band { color: #126a59; }.auth-band div, .settings-title, .settings-value { display: flex; flex-direction: column; gap: 3px; }.auth-band span, .settings-title span, .settings-value small, .field-hint, .setting-switch span, .section-heading span, .probe-option span, .probe-interval small { color: #66717d; font-size: 11px; }.settings-value { align-items: flex-end; }.settings-value span { font-family: 'JetBrains Mono', monospace; font-size: 13px; text-transform: uppercase; }.settings-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-bottom: 1px solid #dce2e7; }.settings-grid div { display: flex; min-height: 72px; flex-direction: column; justify-content: center; gap: 6px; border-right: 1px solid #dce2e7; }.settings-grid div:last-child { border: 0; }.settings-grid span { color: #66717d; font-size: 11px; }.settings-grid strong { font-size: 13px; }.settings-section { padding: 20px 0; border-bottom: 1px solid #dce2e7; }.settings-section.compact { padding: 18px 0; }.section-heading, .setting-switch { display: flex; align-items: center; justify-content: space-between; gap: 18px; }.section-heading h2 { margin: 0 0 4px; color: #15202b; font-size: 14px; }.settings-form { margin-top: 18px; }.settings-form :deep(.el-form-item) { margin-bottom: 10px; }.settings-form :deep(.el-input-number) { width: 100%; }.timeout-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; }.timeout-grid .field-hint { min-height: 34px; }.setting-switch div, .probe-option div { display: flex; flex-direction: column; gap: 4px; }.probe-settings { padding: 18px 0; }.probe-controls { display: grid; grid-template-columns: minmax(150px, .65fr) repeat(2, minmax(230px, 1fr)); align-items: center; gap: 18px; margin-top: 16px; }.probe-interval, .probe-option { display: flex; align-items: center; min-width: 0; gap: 10px; }.probe-interval { white-space: nowrap; }.probe-interval :deep(.el-input-number) { width: 112px; }.probe-option { justify-content: space-between; padding-left: 18px; border-left: 1px solid #dce2e7; }.probe-option div { min-width: 0; }.probe-option strong { font-size: 13px; }.probe-option span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.smtp-test { margin-top: 8px; padding-top: 16px; border-top: 1px solid #dce2e7; }.field-hint { margin: 2px 0 0; line-height: 1.55; }.settings-form :deep(textarea) { font-family: 'JetBrains Mono', monospace; font-size: 12px; }.mail-test-actions { display: flex; width: min(440px, 100%); flex-direction: row !important; gap: 8px !important; }.mail-test-actions .el-input { min-width: 0; }.settings-save-actions { display: flex; justify-content: flex-end; padding-top: 6px; }@media (max-width: 720px) { .settings-toolbar { align-items: flex-start; flex-wrap: wrap; }.settings-toolbar .spacer { display: none; }.settings-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }.settings-grid div:nth-child(2) { border-right: 0; }.settings-grid div:nth-child(-n+2) { border-bottom: 1px solid #dce2e7; }.section-heading { align-items: flex-start; }.settings-form :deep(.el-segmented) { max-width: 100%; height: auto; flex-wrap: wrap; }.timeout-grid { grid-template-columns: 1fr; gap: 0; }.timeout-grid .field-hint { min-height: 0; }.setting-switch { align-items: flex-start; flex-direction: column; }.probe-controls { grid-template-columns: 1fr; gap: 12px; }.probe-option { padding: 12px 0 0; border-top: 1px solid #dce2e7; border-left: 0; }.mail-test-actions { width: 100%; } }@media (max-width: 480px) { .settings-grid { grid-template-columns: 1fr; }.settings-grid div { border-right: 0; border-bottom: 1px solid #dce2e7; }.settings-grid div:last-child { border-bottom: 0; } }
.form-subsection { display: flex; flex-direction: column; gap: 4px; margin: 10px 0 16px; padding-top: 16px; border-top: 1px solid #dce2e7; }.form-subsection strong { color: #15202b; font-size: 13px; }.form-subsection span { color: #66717d; font-size: 11px; }
.sensitive-switch { margin-top: 18px; }.sensitive-switch + .sensitive-switch { padding-top: 16px; border-top: 1px solid #dce2e7; }
</style>
