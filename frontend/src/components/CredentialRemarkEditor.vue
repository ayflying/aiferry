<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { apiPut } from '../api/client'
import { showError, showWarning } from '../lib/error'

const props = defineProps<{ channelId: number; credentialId: number; remark: string }>()
const emit = defineEmits<{ saved: [remark: string] }>()
const editing = ref(false)
const draft = ref('')
const saving = ref(false)
let generation = 0
watch(() => [props.channelId, props.credentialId], () => {
  generation += 1
  editing.value = false
  saving.value = false
}, { flush: 'sync' })
onBeforeUnmount(() => { generation += 1 })

function edit() {
  draft.value = props.remark || ''
  editing.value = true
}

async function save() {
  if (saving.value) return
  if (Array.from(draft.value).length > 500) {
    showWarning('备注不能超过 500 字符')
    return
  }
  const current = generation
  const remark = draft.value.trim()
  saving.value = true
  try {
    await apiPut(`/channels/${props.channelId}/credentials/${props.credentialId}/remark`, { remark })
    if (current !== generation) return
    emit('saved', remark)
    editing.value = false
  } catch (error) {
    if (current === generation) showError(error, '保存密钥备注失败')
  } finally {
    if (current === generation) saving.value = false
  }
}
</script>

<template>
  <div class="credential-remark">
    <template v-if="!editing">
      <span v-if="remark" class="remark-text">{{ remark }}</span>
      <el-button link size="small" @click="edit">{{ remark ? '编辑备注' : '添加备注' }}</el-button>
    </template>
    <template v-else>
      <el-input v-model="draft" type="textarea" :rows="2" maxlength="500" show-word-limit aria-label="密钥备注" placeholder="最多 500 字符，留空保存可清除" :disabled="saving" />
      <div class="remark-actions">
        <el-button size="small" :disabled="saving" @click="editing = false">取消</el-button>
        <el-button size="small" :disabled="saving || !draft" @click="draft = ''">清空</el-button>
        <el-button size="small" type="primary" :loading="saving" @click="save">保存备注</el-button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.credential-remark { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; margin-top: 5px; }
.remark-text { max-width: 100%; white-space: pre-wrap; overflow-wrap: anywhere; color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.5; }
.remark-actions { display: flex; gap: 6px; }
.remark-actions .el-button + .el-button { margin-left: 0; }
</style>
