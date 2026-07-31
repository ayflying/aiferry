<script setup lang="ts">
import { Plus, Route, Trash2 } from '@lucide/vue'

type ModelOption = {
  id: number
  publicName: string
  upstreamName: string
}

defineProps<{
  editing: boolean
  models: ModelOption[]
}>()

const priority = defineModel<number>('priority', { required: true })
const weight = defineModel<number>('weight', { required: true })
const backupBaseUrls = defineModel<string[]>('backupBaseUrls', { required: true })
const healthCheckModelId = defineModel<number>('healthCheckModelId', { required: true })
const autoDisableEnabled = defineModel<boolean>('autoDisableEnabled', { required: true })

function addBackupBaseURL() {
  if (backupBaseUrls.value.length < 8) backupBaseUrls.value.push('')
}

function removeBackupBaseURL(index: number) {
  backupBaseUrls.value.splice(index, 1)
}
</script>

<template>
  <section class="route-coverage-settings">
    <div class="route-heading"><Route :size="17" /><strong>路由与覆盖</strong></div>
    <div class="route-panel">
      <div class="panel-caption">路由策略</div>
      <div class="route-grid">
        <label class="route-field">
          <strong>优先级</strong>
          <el-input-number v-model="priority" :min="-999" :max="999" controls-position="right" />
          <span>优先级更高的渠道优先被选中</span>
        </label>
        <label class="route-field">
          <strong>权重</strong>
          <el-input-number v-model="weight" :min="1" :max="1000" controls-position="right" />
          <span>用于负载均衡。权重越高，请求越多</span>
        </label>
      </div>
      <div class="backup-base-urls">
        <div class="backup-heading"><strong>备用 API 根地址</strong><span>主地址失败时会依次尝试备用 CDN；全部失败后才回退其他渠道。</span></div>
        <div v-for="(baseURL, index) in backupBaseUrls" :key="index" class="backup-base-url-row">
          <el-input v-model="backupBaseUrls[index]" placeholder="https://cdn.example.com/v1" />
          <el-tooltip content="删除备用地址"><button class="icon-button danger" type="button" :aria-label="`删除第 ${index + 1} 个备用地址`" @click="removeBackupBaseURL(index)"><Trash2 :size="16" /></button></el-tooltip>
        </div>
        <el-button :icon="Plus" text :disabled="backupBaseUrls.length >= 8" @click="addBackupBaseURL">添加备用地址</el-button>
      </div>
      <label class="route-field test-model-field">
        <strong>测试模型</strong>
        <el-select v-model="healthCheckModelId" clearable filterable :disabled="!editing || !models.length" :placeholder="editing ? '选择已启用模型' : '保存渠道并启用模型后选择'">
          <el-option v-for="model in models" :key="model.id" :label="model.publicName === model.upstreamName ? model.publicName : `${model.publicName} (${model.upstreamName})`" :value="model.id" />
        </el-select>
	        <span>系统后台探测优先使用此模型验证渠道可用性；未选择时自动使用该渠道首个启用模型。</span>
      </label>
      <div class="auto-disable-row">
        <div><strong>自动封禁</strong><span>重复失败时自动禁用此渠道，仍受系统全局规则约束。</span></div>
        <el-switch v-model="autoDisableEnabled" />
      </div>
    </div>
  </section>
</template>

<style scoped>
.route-coverage-settings { margin: 18px 0; }
.route-heading { display: flex; align-items: center; gap: 8px; padding: 0 0 10px; color: #15202b; }
.route-heading svg { color: #1677ff; }
.route-heading strong, .route-field strong, .auto-disable-row strong { font-size: 13px; }
.route-panel { border: 1px solid #acd8ff; border-radius: 8px; padding: 12px; }
.panel-caption { padding-bottom: 10px; color: #40505f; font-size: 12px; font-weight: 600; }
.route-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px 16px; }
.route-field { display: grid; min-width: 0; gap: 7px; }
.route-field span, .auto-disable-row span { color: #66717d; font-size: 11px; line-height: 1.45; }
.route-field :deep(.el-input-number), .route-field :deep(.el-select) { width: 100%; }
.backup-base-urls { display: grid; gap: 8px; margin-top: 14px; padding-top: 12px; border-top: 1px solid #dceafb; }.backup-heading { display: flex; flex-direction: column; gap: 4px; }.backup-heading strong { font-size: 13px; }.backup-heading span { color: #66717d; font-size: 11px; line-height: 1.45; }.backup-base-url-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; align-items: center; }.icon-button { display: inline-grid; width: 32px; height: 32px; place-items: center; border: 1px solid #dce2e7; border-radius: 5px; color: #40505f; background: #fff; cursor: pointer; }.icon-button.danger { color: #c83e4d; }.icon-button:disabled { cursor: not-allowed; opacity: .5; }
.test-model-field { grid-column: 1 / -1; margin-top: 2px; }
.auto-disable-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 14px; padding-top: 12px; border-top: 1px solid #dceafb; }
.auto-disable-row > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.auto-disable-row :deep(.el-switch) { flex: 0 0 auto; }
@media (max-width: 560px) { .route-grid { grid-template-columns: 1fr; }.test-model-field { grid-column: auto; }.auto-disable-row { align-items: flex-start; }.auto-disable-row :deep(.el-switch) { margin-top: 4px; } }
</style>
