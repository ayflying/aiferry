<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { CircleDollarSign, Pencil, RefreshCw, Trash2, UserRound, UsersRound } from '@lucide/vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPut } from '../api/client'
import type { AccountProfile, ManagedUser } from '../api/types'
import { showError } from '../lib/error'
import { formatCost, formatNumber, formatTime } from '../lib/format'
import { useAuthStore } from '../stores/auth'
import TableActionButton from '../components/TableActionButton.vue'

const auth = useAuthStore()
const loading = ref(false)
const saving = ref(false)
const users = ref<ManagedUser[]>([])
const balanceDialog = ref(false)
const selected = ref<ManagedUser>()
const form = reactive({ balance: 0 })

async function load() {
  loading.value = true
  try { users.value = await apiGet<ManagedUser[]>('/users') } catch (error) { showError(error, '加载用户失败') } finally { loading.value = false }
}

function openBalance(user: ManagedUser) {
  selected.value = user
  form.balance = user.balance
  balanceDialog.value = true
}

async function saveBalance() {
  if (!selected.value) return
  saving.value = true
  try {
    await apiPut<AccountProfile>(`/users/${selected.value.id}/balance`, { balance: form.balance })
    ElMessage.success('用户余额已更新')
    balanceDialog.value = false
    await load()
  } catch (error) { showError(error, '更新用户余额失败') } finally { saving.value = false }
}

async function remove(user: ManagedUser) {
  try {
    await ElMessageBox.confirm(`删除“${user.nickname}”后将永久清理其用量记录、API 密钥及授权策略，无法恢复。`, '删除用户', { type: 'warning', confirmButtonText: '删除用户', cancelButtonText: '取消' })
    await apiDelete<Record<string, never>>(`/users/${user.id}`)
    ElMessage.success('用户及关联数据已删除')
    await load()
  } catch (error) { if (error !== 'cancel') showError(error, '删除用户失败') }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar"><div class="muted">管理 Casdoor 登录用户的账户余额与本地关联数据</div><div class="spacer" /><el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button></div>
    <div class="table-panel">
      <el-table v-loading="loading" :data="users" row-key="id">
        <el-table-column label="用户" min-width="190"><template #default="{ row }"><div class="user-cell"><el-avatar :size="30" :src="row.avatarUrl || undefined"><UserRound :size="15" /></el-avatar><div><strong>{{ row.nickname }}</strong><small>{{ row.role === 'admin' ? '管理员' : '用户' }}</small></div></div></template></el-table-column>
        <el-table-column label="邮箱" min-width="190"><template #default="{ row }">{{ row.email || '未绑定' }}</template></el-table-column>
        <el-table-column label="余额" min-width="132"><template #default="{ row }"><span class="mono">{{ formatCost(row.balance) }}</span></template></el-table-column>
        <el-table-column label="访问密钥" width="110" align="right"><template #default="{ row }">{{ formatNumber(row.apiKeyCount) }}</template></el-table-column>
        <el-table-column label="近 30 天调用" min-width="130" align="right"><template #default="{ row }"><div class="usage-cell"><strong>{{ formatNumber(row.usage.requests) }}</strong><small>{{ formatCost(row.usage.estimatedCost) }}</small></div></template></el-table-column>
        <el-table-column label="最近登录" min-width="170"><template #default="{ row }">{{ formatTime(row.lastLoginAt) }}</template></el-table-column>
        <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><TableActionButton :icon="CircleDollarSign" label="修改余额" @click="openBalance(row)" /><TableActionButton v-if="row.id !== auth.user?.id" :icon="Trash2" label="删除用户" danger @click="remove(row)" /></div></template></el-table-column>
      </el-table>
      <div v-if="!loading && !users.length" class="empty-state"><div><UsersRound :size="28" /><strong>尚无 Casdoor 登录用户</strong><span>用户首次完成 Casdoor 登录后会显示在这里</span></div></div>
    </div>

    <el-dialog v-model="balanceDialog" title="修改用户余额" width="min(440px, 92vw)">
      <el-form label-position="top"><el-form-item label="账户余额（USD）"><el-input-number v-model="form.balance" :min="0" :precision="6" :controls="false" style="width: 100%" /></el-form-item></el-form>
      <template #footer><el-button @click="balanceDialog = false">取消</el-button><el-button type="primary" :icon="Pencil" :loading="saving" @click="saveBalance">保存余额</el-button></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.user-cell { display: flex; align-items: center; gap: 9px; }.user-cell div, .usage-cell { display: flex; flex-direction: column; gap: 2px; }.user-cell strong { color: #15202b; font-size: 12px; }.user-cell small, .usage-cell small { color: #7b8792; font-size: 10px; }.usage-cell { align-items: flex-end; font-family: 'JetBrains Mono', monospace; font-size: 11px; }.empty-state svg { display: block; margin: 0 auto 10px; color: #7b8792; }.empty-state span { display: block; }
</style>
