<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ModelMetadata, ModelMetadataResponse, ModelModality } from '../api/types/model'

const props = defineProps<{
  data: ModelMetadataResponse | null
  loading: boolean
  saving: boolean
}>()
const emit = defineEmits<{
  save: [metadata: ModelMetadata | null]
  cancel: []
  retry: []
}>()
const modalities: ModelModality[] = ['text', 'image', 'audio', 'video', 'file']
const modalityFields = [
  { key: 'input_modalities', label: '输入模态' },
  { key: 'output_modalities', label: '输出模态' },
] as const
const capabilityFields = [
  { key: 'tools', label: '工具调用' },
  { key: 'reasoning', label: '推理' },
  { key: 'structured_output', label: '结构化输出' },
] as const
const tokenFields = [
  { key: 'context_length', label: '上下文长度' },
  { key: 'max_output_tokens', label: '最大输出 Token' },
] as const
function emptyMetadata(): ModelMetadata {
  return { display_name: null, description: null, input_modalities: null, output_modalities: null, context_length: null, max_output_tokens: null, capabilities: { tools: null, reasoning: null, structured_output: null } }
}
const draft = ref<ModelMetadata>(emptyMetadata())
watch(() => props.data, (data) => {
  const manual = data?.manual ?? emptyMetadata()
  draft.value = {
    ...manual,
    input_modalities: manual.input_modalities === null ? null : [...manual.input_modalities],
    output_modalities: manual.output_modalities === null ? null : [...manual.output_modalities],
    capabilities: { ...manual.capabilities },
  }
}, { immediate: true })
function tokenInvalid(value: number | null) {
  return value !== null && (!Number.isSafeInteger(value) || value <= 0)
}
const invalidTokens = computed(() => tokenFields.some(({ key }) => tokenInvalid(draft.value[key])))
function setText(key: 'display_name' | 'description', value: string) {
  draft.value[key] = value.trim() ? value : null
}
function setModalityMode(key: 'input_modalities' | 'output_modalities', mode: string) {
  draft.value[key] = mode === 'unknown' ? null : draft.value[key] ?? []
}
function setCapability(key: keyof ModelMetadata['capabilities'], value: string) {
  draft.value.capabilities[key] = value === 'unknown' ? null : value === 'yes'
}
function capabilityValue(value: boolean | null) {
  return value === null ? 'unknown' : value ? 'yes' : 'no'
}
function displayValue(value: string | number | boolean | ModelModality[] | null) {
  if (value === null) return '未知'
  if (typeof value === 'boolean') return value ? '支持' : '不支持'
  if (Array.isArray(value)) return value.length ? value.join(' / ') : '明确无'
  return String(value)
}
const previewRows = computed(() => {
  if (!props.data) return []
  const { automatic, effective } = props.data
  const fields = [
    { key: 'display_name', label: '显示名称' },
    { key: 'description', label: '描述' },
    ...modalityFields, ...tokenFields,
  ] as const
  return [
    ...fields.map(({ key, label }) => ({ label, automatic: displayValue(automatic[key]), effective: displayValue(effective[key]) })),
    ...capabilityFields.map(({ key, label }) => ({ label, automatic: displayValue(automatic.capabilities[key]), effective: displayValue(effective.capabilities[key]) })),
  ]
})
function submit() {
  if (invalidTokens.value || props.saving || props.loading) return
  emit('save', {
    ...draft.value,
    display_name: draft.value.display_name?.trim() || null,
    description: draft.value.description?.trim() || null,
    input_modalities: draft.value.input_modalities === null ? null : [...draft.value.input_modalities],
    output_modalities: draft.value.output_modalities === null ? null : [...draft.value.output_modalities],
    capabilities: { ...draft.value.capabilities },
  })
}
</script>

<template>
  <div v-loading="loading" class="metadata-editor" :aria-busy="loading || saving">
    <template v-if="data">
      <p class="metadata-help">自动值仅来自已记录的元数据，不根据模型名称推测。手动字段留空或选“未知”时恢复该字段的自动值；自动值也为空时才为未知。</p>
      <details class="metadata-preview" open>
        <summary>自动值与已保存生效值</summary>
        <div class="metadata-preview__scroll"><table>
          <thead><tr><th scope="col">字段</th><th scope="col">自动值</th><th scope="col">已保存生效值</th></tr></thead>
          <tbody><tr v-for="row in previewRows" :key="row.label"><th scope="row">{{ row.label }}</th><td>{{ row.automatic }}</td><td>{{ row.effective }}</td></tr></tbody>
        </table></div>
      </details>
      <el-form label-position="top" :disabled="loading || saving" @submit.prevent="submit">
        <div class="metadata-grid">
          <el-form-item label="显示名称（手动）"><el-input :model-value="draft.display_name ?? ''" clearable placeholder="留空恢复自动" @update:model-value="setText('display_name', $event)" /></el-form-item>
          <el-form-item label="描述（手动）" class="metadata-wide"><el-input :model-value="draft.description ?? ''" type="textarea" :rows="2" placeholder="留空恢复自动" @update:model-value="setText('description', $event)" /></el-form-item>
          <el-form-item v-for="field in modalityFields" :key="field.key" :label="`${field.label}（手动）`">
            <div class="metadata-modality">
              <el-select :model-value="draft[field.key] === null ? 'unknown' : 'list'" :aria-label="`${field.label}模式`" @update:model-value="setModalityMode(field.key, $event)">
                <el-option label="未知（恢复自动）" value="unknown" /><el-option label="明确列表（可为空）" value="list" />
              </el-select>
              <el-select v-if="draft[field.key] !== null" :model-value="draft[field.key]" multiple clearable :aria-label="`${field.label}列表`" placeholder="明确无（空列表）" @update:model-value="draft[field.key] = $event">
                <el-option v-for="modality in modalities" :key="modality" :label="modality" :value="modality" />
              </el-select>
            </div>
          </el-form-item>
          <el-form-item v-for="field in capabilityFields" :key="field.key" :label="`${field.label}（手动）`">
            <el-select :model-value="capabilityValue(draft.capabilities[field.key])" @update:model-value="setCapability(field.key, $event)">
              <el-option label="未知（恢复自动）" value="unknown" /><el-option label="支持" value="yes" /><el-option label="不支持" value="no" />
            </el-select>
          </el-form-item>
          <el-form-item v-for="field in tokenFields" :key="field.key" :label="`${field.label}（手动）`" :error="tokenInvalid(draft[field.key]) ? '请输入正的安全整数，或留空恢复自动' : undefined">
            <el-input-number :model-value="draft[field.key] ?? undefined" :min="1" :max="Number.MAX_SAFE_INTEGER" :step="1" :controls="false" placeholder="留空恢复自动" @update:model-value="draft[field.key] = $event ?? null" />
          </el-form-item>
        </div>
        <p class="metadata-help">选择“明确列表”后不选任何模态会保存 []（明确无），与未知不同。保存仅修改当前公开模型的手动元数据，不影响价格。</p>
      </el-form>
    </template>
    <div v-else-if="!loading" class="metadata-empty"><p>未能加载模型能力，请重试。</p><el-button @click="emit('retry')">重新加载</el-button></div>
    <div v-else class="metadata-empty">正在加载模型能力…</div>
    <div class="metadata-actions">
      <el-button :disabled="!data || loading || saving || data.manual === null" @click="emit('save', null)">清除覆盖，恢复自动</el-button>
      <div class="spacer" />
      <el-button :disabled="saving" @click="emit('cancel')">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="!data || loading || invalidTokens" @click="submit">保存能力</el-button>
    </div>
  </div>
</template>

<style scoped>
.metadata-editor { color: #253343; }
.metadata-help { margin: 0 0 14px; color: #66717d; font-size: 12px; line-height: 1.6; }
.metadata-preview { margin-bottom: 18px; border: 1px solid #dce2e7; border-radius: 6px; }
.metadata-preview summary { padding: 10px 12px; background: #f7f9fa; cursor: pointer; font-weight: 600; }
.metadata-preview__scroll { max-height: 260px; overflow: auto; }
.metadata-preview table { width: 100%; border-collapse: collapse; text-align: left; font-size: 12px; }
.metadata-preview th, .metadata-preview td { padding: 8px 12px; border-bottom: 1px solid #e5e9ed; vertical-align: top; overflow-wrap: anywhere; white-space: pre-wrap; }
.metadata-preview th { width: 24%; font-weight: 600; }
.metadata-preview td { width: 38%; }
.metadata-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 16px; }
.metadata-wide { grid-column: 1 / -1; }
.metadata-grid :deep(.el-select), .metadata-grid :deep(.el-input-number) { width: 100%; }
.metadata-modality { display: grid; width: 100%; gap: 8px; }
.metadata-actions { display: flex; flex-wrap: wrap; gap: 8px; padding-top: 14px; border-top: 1px solid #dce2e7; }
.metadata-actions :deep(.el-button + .el-button) { margin-left: 0; }
.metadata-empty { min-height: 120px; padding: 24px 0; text-align: center; color: #66717d; }
@media (max-width: 600px) { .metadata-grid { grid-template-columns: 1fr; } .metadata-actions .spacer { display: none; } }
</style>
